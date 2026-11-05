package events_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/events"
	"github.com/raveesh-me/dedo/events/eventstest"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/policy"
)

// TestBehindInterceptor publishes from handlers served behind the flow ID
// and policy interceptors, as a service does: the contract each RPC
// declares decides what it may publish.
func TestBehindInterceptor(t *testing.T) {
	files, err := testsvc.Files()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.Service)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        func(context.Context) []string { return []string{testsvc.ReadScope, testsvc.WriteScope} },
		EnforceScopes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	d := eventstest.New()
	pub, err := events.New(events.Config{Driver: d, Source: "//test.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	publish := func(ctx context.Context) error {
		_, err := pub.Publish(ctx, testsvc.Deleted, "things/1")
		return err
	}
	server := connect.NewServer(flowid.Interceptor(), guard)
	for _, p := range []string{testsvc.GetThing, testsvc.DeleteThing} {
		server.Register(connect.Method{
			Spec: connect.Spec{Procedure: p, StreamType: connect.StreamTypeUnary},
			Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
				if err := stream.Receive(&emptypb.Empty{}); err != nil {
					return err
				}
				if err := publish(ctx); err != nil {
					return err
				}
				return stream.Send(&emptypb.Empty{})
			},
		})
	}
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, srv.URL))
	call := func(procedure string) error {
		ctx, info := connect.NewClientContext(t.Context())
		info.RequestHeader().Set(flowid.Header, "flow-7")
		return client.CallUnary(ctx, connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary}, &emptypb.Empty{}, &emptypb.Empty{})
	}

	if err := call(testsvc.DeleteThing); err != nil {
		t.Fatalf("DeleteThing declares %s: %v", testsvc.Deleted, err)
	}
	got := d.Events()
	if len(got) != 1 || got[0].Type != testsvc.Deleted || got[0].Extensions["flowid"] != "flow-7" {
		t.Fatalf("published %+v", got)
	}

	err = call(testsvc.GetThing)
	var cerr *connect.Error
	if !errors.As(err, &cerr) || cerr.Code() != connect.CodeInternal {
		t.Fatalf("GetThing declares no events, but publishing returned %v", err)
	}
	if n := len(d.Attempts()); n != 1 {
		t.Fatalf("an undeclared event reached the driver: %d attempts", n)
	}
}
