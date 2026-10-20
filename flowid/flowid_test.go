package flowid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/flowid"
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

// echoServer records the flow ID each call's handler sees.
func echoServer(seen *string) *connect.Server {
	server := connect.NewServer(flowid.Interceptor())
	server.Register(connect.Method{
		Spec: connect.Spec{Procedure: "/test.v1.S/M", StreamType: connect.StreamTypeUnary},
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			*seen = flowid.From(ctx)
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			return stream.Send(&emptypb.Empty{})
		},
	})
	return server
}

func callEcho(t *testing.T, server *connect.Server, ctx context.Context, inbound string, interceptors ...connect.ClientInterceptor) (echoed string) {
	t.Helper()
	client := connect.NewClient(connectinprocess.New(server), interceptors...)
	ctx, info := connect.NewClientContext(ctx)
	if inbound != "" {
		info.RequestHeader().Set(flowid.Header, inbound)
	}
	spec := connect.Spec{Procedure: "/test.v1.S/M", StreamType: connect.StreamTypeUnary}
	if err := client.CallUnary(ctx, spec, &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	return info.ResponseHeader().Get(flowid.Header)
}

func TestInterceptorKeepsValidID(t *testing.T) {
	var seen string
	if echoed := callEcho(t, echoServer(&seen), t.Context(), "flow-1"); seen != "flow-1" || echoed != "flow-1" {
		t.Fatalf("seen %q, echoed %q", seen, echoed)
	}
}

func TestInterceptorReplacesMalformedID(t *testing.T) {
	var seen string
	echoed := callEcho(t, echoServer(&seen), t.Context(), "bad id\n")
	if seen == "" || seen == "bad id\n" || echoed != seen {
		t.Fatalf("seen %q, echoed %q", seen, echoed)
	}
}

func TestClientInterceptorPropagatesID(t *testing.T) {
	var seen string
	ctx := flowid.WithID(t.Context(), "flow-2")
	callEcho(t, echoServer(&seen), ctx, "", flowid.ClientInterceptor())
	if seen != "flow-2" {
		t.Fatalf("server saw flow ID %q", seen)
	}
}
