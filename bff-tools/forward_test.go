package bfftools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/types/known/emptypb"

	bfftools "github.com/raveesh-me/dedo/bff-tools"
	"github.com/raveesh-me/dedo/flowid"
)

const backendMethod = "/shop.orders.v1.OrderService/GetOrder"

func unarySpec(procedure string) connect.Spec {
	return connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary}
}

// backend records the request headers of each call it serves.
func backend(t *testing.T, seen *http.Header) string {
	t.Helper()
	srv := connect.NewServer()
	srv.Register(connect.Method{
		Spec: unarySpec(backendMethod),
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			info, _ := connect.CallInfoForServerContext(ctx)
			*seen = http.Header{}
			for k, v := range info.RequestHeader().All() {
				(*seen)[k] = slices.Clone(v)
			}
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			return stream.Send(&emptypb.Empty{})
		},
	})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL
}

// bffServer is a BFF whose one method calls the backend with a client
// carrying forward, after setting outbound on the call itself.
func bffServer(t *testing.T, backendURL string, forward connect.ClientInterceptor, outbound map[string]string) string {
	t.Helper()
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, backendURL), forward)
	srv := connect.NewServer(flowid.Interceptor())
	srv.Register(connect.Method{
		Spec: unarySpec(method),
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			if len(outbound) > 0 {
				var info *connect.CallInfo
				ctx, info = connect.NewClientContext(ctx)
				for k, v := range outbound {
					info.RequestHeader().Set(k, v)
				}
			}
			if err := client.CallUnary(ctx, unarySpec(backendMethod), &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
				return err
			}
			return stream.Send(&emptypb.Empty{})
		},
	})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, srv)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestForward(t *testing.T) {
	tests := []struct {
		name     string
		headers  []string          // Forward's arguments
		inbound  map[string]string // the frontend's request headers
		outbound map[string]string // headers the BFF's handler sets itself
		want     map[string]string // the backend's request headers; "" is absent
	}{
		{
			name:    "defaults forward credentials and flow ID",
			inbound: map[string]string{"Authorization": "Bearer user", flowid.Header: "flow-1", "X-Other": "x"},
			want:    map[string]string{"Authorization": "Bearer user", flowid.Header: "flow-1", "X-Other": ""},
		},
		{
			name:    "no credentials are invented",
			inbound: map[string]string{flowid.Header: "flow-1"},
			want:    map[string]string{"Authorization": "", flowid.Header: "flow-1"},
		},
		{
			name:    "a malformed flow ID is forwarded as the BFF replaced it",
			inbound: map[string]string{"Authorization": "Bearer user", flowid.Header: "bad id"},
			want:    map[string]string{"Authorization": "Bearer user", flowid.Header: "replaced"},
		},
		{
			name:    "named headers on top of credentials and flow ID",
			headers: []string{"authorization", "X-Tenant"},
			inbound: map[string]string{"Authorization": "Bearer user", "X-Tenant": "t1", flowid.Header: "flow-1", "X-Other": "x"},
			want:    map[string]string{"Authorization": "Bearer user", "X-Tenant": "t1", flowid.Header: "flow-1", "X-Other": ""},
		},
		{
			name:     "the caller's credentials replace the handler's",
			inbound:  map[string]string{"Authorization": "Bearer user"},
			outbound: map[string]string{"Authorization": "Bearer service", "X-Kept": "k"},
			want:     map[string]string{"Authorization": "Bearer user", "X-Kept": "k"},
		},
		{
			name:     "an absent header leaves the handler's",
			outbound: map[string]string{"Authorization": "Bearer set"},
			want:     map[string]string{"Authorization": "Bearer set"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen http.Header
			url := bffServer(t, backend(t, &seen), bfftools.Forward(tt.headers...), tt.outbound)
			client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, url))
			ctx, info := connect.NewClientContext(t.Context())
			for k, v := range tt.inbound {
				info.RequestHeader().Set(k, v)
			}
			if err := client.CallUnary(ctx, unarySpec(method), &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
				t.Fatal(err)
			}
			for k, want := range tt.want {
				got := seen.Get(k)
				switch {
				case want == "replaced" && (got == "" || got != info.ResponseHeader().Get(flowid.Header)):
					t.Errorf("%s = %q, want the BFF's flow ID %q", k, got, info.ResponseHeader().Get(flowid.Header))
				case want != "replaced" && got != want:
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		})
	}
}

// Every value of a repeated header goes on, and outside a served call
// Forward adds nothing.
func TestForwardValuesAndOutsideCall(t *testing.T) {
	var seen http.Header
	url := bffServer(t, backend(t, &seen), bfftools.Forward("X-Many"), nil)
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, url))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Add("X-Many", "a")
	info.RequestHeader().Add("X-Many", "b")
	if err := client.CallUnary(ctx, unarySpec(method), &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if got := seen.Values("X-Many"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("X-Many = %q, want [a b]", got)
	}

	direct := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, backend(t, &seen)), bfftools.Forward())
	if err := direct.CallUnary(t.Context(), unarySpec(backendMethod), &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("Authorization"); got != "" {
		t.Errorf("outside a served call, Authorization = %q, want none", got)
	}
}
