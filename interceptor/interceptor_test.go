package interceptor_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/policy"
)

// handlerFunc is a unary handler of the test service, which takes and
// returns Empty.
type handlerFunc func(context.Context) error

func ok(context.Context) error { return nil }

// unary adapts a handlerFunc to a server function, as generated code does.
func unary(h handlerFunc) connect.ServerFunc {
	return func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
		if err := stream.Receive(&emptypb.Empty{}); err != nil {
			return err
		}
		if err := h(ctx); err != nil {
			return err
		}
		return stream.Send(&emptypb.Empty{})
	}
}

func spec(procedure string) connect.Spec {
	return connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary, IdempotencyLevel: connect.IdempotencyNoSideEffects}
}

type fixture struct {
	url string
	log *bytes.Buffer
}

func newFixture(t *testing.T, enforce bool, scopes []string, handler handlerFunc) fixture {
	t.Helper()
	files, err := testsvc.Files()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.Service)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        func(context.Context) []string { return scopes },
		EnforceScopes: enforce,
		Log:           slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer(guard)
	for _, p := range []string{testsvc.GetThing, testsvc.DeleteThing, "/raveesh_me.test.v1.TestService/Unlisted"} {
		server.Register(connect.Method{Spec: spec(p), Handler: unary(handler)})
	}
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fixture{url: srv.URL, log: &log}
}

// call makes a unary call and returns the response headers.
func (f fixture) call(t *testing.T, procedure string, opts ...connecthttp.Option) (*connect.Header, error) {
	t.Helper()
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, f.url, opts...))
	ctx, info := connect.NewClientContext(t.Context())
	err := client.CallUnary(ctx, spec(procedure), &emptypb.Empty{}, &emptypb.Empty{})
	return info.ResponseHeader(), err
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	if _, err := interceptor.New(interceptor.Config{}); err == nil {
		t.Error("want error for empty table")
	}
	table := policy.Table{"/x/y": {}}
	if _, err := interceptor.New(interceptor.Config{Table: table}); err == nil {
		t.Error("want error for missing scope source")
	}
}

func TestEnforcedScopeDenied(t *testing.T) {
	f := newFixture(t, true, []string{"other.thing.read"}, ok)
	_, err := f.call(t, testsvc.GetThing)
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("err = %v, want permission_denied", err)
	}
}

func TestAuditModeAllowsAndLogs(t *testing.T) {
	f := newFixture(t, false, nil, ok)
	if _, err := f.call(t, testsvc.GetThing); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.log.String(), "scope check failed in audit mode") {
		t.Fatalf("log = %q", f.log.String())
	}
}

func TestUnlistedProcedureUnimplemented(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.ReadScope}, ok)
	_, err := f.call(t, "/raveesh_me.test.v1.TestService/Unlisted")
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("err = %v, want unimplemented", err)
	}
}

func TestCacheHeaderOnGetOnly(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.ReadScope}, ok)
	h, err := f.call(t, testsvc.GetThing, connecthttp.WithHTTPGet())
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Cache-Control"); got != "public, max-age=30" {
		t.Errorf("GET Cache-Control = %q", got)
	}
	h, err = f.call(t, testsvc.GetThing)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Cache-Control"); got != "" {
		t.Errorf("POST Cache-Control = %q, want none", got)
	}
}

// An error is never cached, even on a GET.
func TestNoCacheHeaderOnError(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.ReadScope}, func(context.Context) error {
		return connect.NewError(connect.CodeNotFound, "failed")
	})
	h, err := f.call(t, testsvc.GetThing, connecthttp.WithHTTPGet())
	if err == nil {
		t.Fatal("want error")
	}
	if got := h.Get("Cache-Control"); got != "" {
		t.Errorf("error Cache-Control = %q, want none", got)
	}
}

func TestDeprecationHeaders(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.WriteScope}, ok)
	h, err := f.call(t, testsvc.DeleteThing)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Deprecation"); got != "@1767225600" {
		t.Errorf("Deprecation = %q", got)
	}
	if got := h.Get("Sunset"); got != "Fri, 01 Jan 2027 00:00:00 GMT" {
		t.Errorf("Sunset = %q", got)
	}
}

// A caller of a deprecated method learns of it from errors too.
func TestDeprecationHeadersOnError(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.WriteScope}, func(context.Context) error {
		return connect.NewError(connect.CodeInternal, "failed")
	})
	h, err := f.call(t, testsvc.DeleteThing)
	if err == nil {
		t.Fatal("want error")
	}
	if got := h.Get("Deprecation"); got != "@1767225600" {
		t.Errorf("error Deprecation = %q", got)
	}
}

func errWithReason(t *testing.T, code connect.Code, reason string) handlerFunc {
	t.Helper()
	return func(context.Context) error {
		detail, err := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		return connect.NewError(code, "failed").WithDetail(detail)
	}
}

func TestErrorAudit(t *testing.T) {
	tests := []struct {
		name    string
		handler handlerFunc
		wantLog string
	}{
		{"declared", errWithReason(t, connect.CodeNotFound, testsvc.Reason), ""},
		{"wrong code", errWithReason(t, connect.CodeInternal, testsvc.Reason), "undeclared error reason"},
		{"unknown reason", errWithReason(t, connect.CodeNotFound, "OTHER"), "undeclared error reason"},
		{"no detail", func(context.Context) error {
			return connect.NewError(connect.CodeNotFound, "failed")
		}, "error without ErrorInfo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, true, []string{testsvc.ReadScope}, tt.handler)
			if _, err := f.call(t, testsvc.GetThing); err == nil {
				t.Fatal("want error")
			}
			got := f.log.String()
			if tt.wantLog == "" && got != "" {
				t.Fatalf("unexpected log %q", got)
			}
			if !strings.Contains(got, tt.wantLog) {
				t.Fatalf("log = %q, want %q", got, tt.wantLog)
			}
		})
	}
}
