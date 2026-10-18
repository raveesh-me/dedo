package flowid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/http-proto-lint/flowid"
)

func serve(t *testing.T, inbound string) (seen, echoed string) {
	t.Helper()
	h := flowid.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = flowid.From(r.Context())
		if r.Header.Get(flowid.Header) != seen {
			t.Errorf("request header %q != context %q", r.Header.Get(flowid.Header), seen)
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if inbound != "" {
		req.Header.Set(flowid.Header, inbound)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return seen, rec.Header().Get(flowid.Header)
}

func TestMiddlewareKeepsValidID(t *testing.T) {
	seen, echoed := serve(t, "abc-123_XYZ")
	if seen != "abc-123_XYZ" || echoed != seen {
		t.Fatalf("seen=%q echoed=%q", seen, echoed)
	}
}

func TestMiddlewareReplacesMissingOrMalformedID(t *testing.T) {
	for _, inbound := range []string{"", "bad id\nINJECTED", string(make([]byte, 200))} {
		seen, echoed := serve(t, inbound)
		if seen == "" || seen == inbound || echoed != seen {
			t.Errorf("inbound=%q seen=%q echoed=%q", inbound, seen, echoed)
		}
	}
}

func TestMiddlewareGeneratesUniqueIDs(t *testing.T) {
	a, _ := serve(t, "")
	b, _ := serve(t, "")
	if a == b {
		t.Fatalf("generated IDs collide: %q", a)
	}
}

func TestFromEmptyContext(t *testing.T) {
	if got := flowid.From(context.Background()); got != "" {
		t.Fatalf("From = %q", got)
	}
}

func TestClientInterceptorPropagatesID(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.Handle("/test.v1.S/M", connect.NewUnaryHandler("/test.v1.S/M",
		func(_ context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			got = req.Header().Get(flowid.Header)
			return connect.NewResponse(&emptypb.Empty{}), nil
		}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := connect.NewClient[emptypb.Empty, emptypb.Empty](srv.Client(), srv.URL+"/test.v1.S/M",
		connect.WithInterceptors(flowid.ClientInterceptor()))
	ctx := flowid.WithID(context.Background(), "flow-1")
	if _, err := client.CallUnary(ctx, connect.NewRequest(&emptypb.Empty{})); err != nil {
		t.Fatal(err)
	}
	if got != "flow-1" {
		t.Fatalf("server saw flow ID %q", got)
	}
}
