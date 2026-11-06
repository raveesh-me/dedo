package bfftools_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/emptypb"

	bfftools "github.com/raveesh-me/dedo/bff-tools"
)

const (
	sha    = "0123456789ab"
	other  = "ba9876543210"
	prefix = "/shop/bff"
	method = "/shop.web.v1.WebService/GetHome"
)

func TestCheckSHA(t *testing.T) {
	tests := []struct {
		sha  string
		want bool
	}{
		{sha, true},
		{"abcdefabcdef", true},
		{"", false},
		{"0123456789a", false},   // too short
		{"0123456789abc", false}, // too long
		{"0123456789AB", false},  // uppercase
		{"0123456789ag", false},  // not hex
		{"0123456789abcdef0123456789abcdef01234567", false}, // full SHA
	}
	for _, tt := range tests {
		if err := bfftools.CheckSHA(tt.sha); (err == nil) != tt.want {
			t.Errorf("CheckSHA(%q) = %v, want ok %v", tt.sha, err, tt.want)
		}
	}
}

func TestPinRejectsBadConstruction(t *testing.T) {
	tests := []struct{ name, sha, prefix string }{
		{"empty SHA", "", prefix},
		{"short SHA", "abc", prefix},
		{"uppercase SHA", "0123456789AB", prefix},
		{"prefix without leading slash", sha, "shop/bff"},
		{"prefix with trailing slash", sha, "/shop/bff/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Pin did not panic")
				}
			}()
			bfftools.Pin(tt.sha, tt.prefix, http.NotFoundHandler())
		})
	}
}

// echo answers with the path and raw path it was given.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, r.URL.Path+" "+r.URL.RawPath)
})

func TestPinRoutes(t *testing.T) {
	tests := []struct {
		name, prefix, path string
		status             int
		body               string // the echoed path, for served requests
	}{
		{"own SHA", prefix, prefix + "/" + sha + method, http.StatusOK, method + " "},
		{"own SHA, no rest", prefix, prefix + "/" + sha, http.StatusOK, "/ "},
		{"own SHA, trailing slash", prefix, prefix + "/" + sha + "/", http.StatusOK, "/ "},
		{"escaped rest", prefix, prefix + "/" + sha + "/a%2Fb", http.StatusOK, "/a/b /a%2Fb"},
		{"empty prefix", "", "/" + sha + method, http.StatusOK, method + " "},
		{"other SHA", prefix, prefix + "/" + other + method, http.StatusBadRequest, ""},
		{"SHA prefix of own", prefix, prefix + "/" + sha[:6] + method, http.StatusBadRequest, ""},
		{"own SHA extended", prefix, prefix + "/" + sha + "0" + method, http.StatusBadRequest, ""},
		{"no SHA", prefix, prefix + method, http.StatusBadRequest, ""},
		{"prefix only", prefix, prefix, http.StatusBadRequest, ""},
		{"prefix and slash", prefix, prefix + "/", http.StatusBadRequest, ""},
		{"latest is no alias", prefix, prefix + "/latest" + method, http.StatusBadRequest, ""},
		{"outside prefix", prefix, "/other/" + sha + method, http.StatusNotFound, ""},
		{"prefix is a word prefix", prefix, prefix + "x/" + sha + method, http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			path := req.URL.Path
			rec := httptest.NewRecorder()
			bfftools.Pin(sha, tt.prefix, echo).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.status, rec.Body)
			}
			if tt.status == http.StatusOK && rec.Body.String() != tt.body {
				t.Errorf("next saw %q, want %q", rec.Body, tt.body)
			}
			if tt.status == http.StatusBadRequest {
				if got := rec.Header().Get("Cache-Control"); got != "no-store" {
					t.Errorf("Cache-Control = %q, want no-store", got)
				}
			}
			if req.URL.Path != path {
				t.Errorf("Pin changed the caller's request path to %q", req.URL.Path)
			}
		})
	}
}

// The retired answer is a Connect error as the Connect protocol writes it,
// which connect-web turns into a ConnectError with the ErrorInfo.
func TestPinRetiredWireFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, prefix+"/"+other+method, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	rec := httptest.NewRecorder()
	bfftools.Pin(sha, prefix, echo).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, Connect's for failed_precondition", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	if body.Code != "failed_precondition" || body.Message == "" {
		t.Errorf("code, message = %q, %q", body.Code, body.Message)
	}
	if len(body.Details) != 1 || body.Details[0].Type != "google.rpc.ErrorInfo" || body.Details[0].Value == "" {
		t.Errorf("details = %+v, want one google.rpc.ErrorInfo", body.Details)
	}
}

// server is a BFF version sha behind Pin, answering one unary method.
func server(t *testing.T) string {
	t.Helper()
	srv := connect.NewServer()
	srv.Register(connect.Method{
		Spec: connect.Spec{Procedure: method, StreamType: connect.StreamTypeUnary, IdempotencyLevel: connect.IdempotencyNoSideEffects},
		Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			return stream.Send(&emptypb.Empty{})
		},
	})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv)
	ts := httptest.NewServer(bfftools.Pin(sha, prefix, mux))
	t.Cleanup(ts.Close)
	return ts.URL
}

// A connect-go client of another version, on each protocol a browser can
// speak, gets FAILED_PRECONDITION with the retired ErrorInfo; one of this
// version is served.
func TestPinRealClient(t *testing.T) {
	url := server(t)
	protocols := []struct {
		name string
		opts []connecthttp.Option
	}{
		{"connect", nil},
		{"connect GET", []connecthttp.Option{connecthttp.WithHTTPGet()}},
		{"connect JSON", []connecthttp.Option{connecthttp.WithProtoJSON()}},
		{"grpc-web", []connecthttp.Option{connecthttp.WithGRPCWeb()}},
	}
	for _, p := range protocols {
		t.Run(p.name, func(t *testing.T) {
			call := func(version string) error {
				client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, url+prefix+"/"+version, p.opts...))
				spec := connect.Spec{Procedure: method, StreamType: connect.StreamTypeUnary, IdempotencyLevel: connect.IdempotencyNoSideEffects}
				return client.CallUnary(t.Context(), spec, &emptypb.Empty{}, &emptypb.Empty{})
			}
			if err := call(sha); err != nil {
				t.Fatalf("own version: %v", err)
			}
			err := call(other)
			if connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("other version: err = %v, want failed_precondition", err)
			}
			if info := errorInfo(t, err); info.GetReason() != bfftools.ReasonVersionRetired || info.GetDomain() != bfftools.ErrorDomain {
				t.Errorf("ErrorInfo = %v, want reason %s in %s", info, bfftools.ReasonVersionRetired, bfftools.ErrorDomain)
			}
		})
	}
}

func errorInfo(t *testing.T, err error) *errdetails.ErrorInfo {
	t.Helper()
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("%v is not a Connect error", err)
	}
	for _, d := range cerr.Details() {
		msg, err := connectproto.UnmarshalErrorDetail(d)
		if err != nil {
			t.Fatal(err)
		}
		if info, ok := msg.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	t.Fatalf("%v carries no ErrorInfo", err)
	return nil
}
