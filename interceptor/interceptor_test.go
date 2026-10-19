package interceptor_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/policy"
)

type handlerFunc func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error)

func ok(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	return connect.NewResponse(&emptypb.Empty{}), nil
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
	mux := http.NewServeMux()
	for _, p := range []string{testsvc.GetThing, testsvc.DeleteThing, "/raveesh_me.test.v1.TestService/Unlisted"} {
		mux.Handle(p, connect.NewUnaryHandler(p, handler,
			connect.WithInterceptors(guard),
			connect.WithIdempotency(connect.IdempotencyNoSideEffects)))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fixture{url: srv.URL, log: &log}
}

func (f fixture) call(t *testing.T, procedure string, opts ...connect.ClientOption) (*connect.Response[emptypb.Empty], error) {
	t.Helper()
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, f.url+procedure, opts...)
	return client.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
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
	res, err := f.call(t, testsvc.GetThing, connect.WithHTTPGet(), connect.WithIdempotency(connect.IdempotencyNoSideEffects))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=30" {
		t.Errorf("GET Cache-Control = %q", got)
	}
	res, err = f.call(t, testsvc.GetThing)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Cache-Control"); got != "" {
		t.Errorf("POST Cache-Control = %q, want none", got)
	}
}

func TestDeprecationHeaders(t *testing.T) {
	f := newFixture(t, true, []string{testsvc.WriteScope}, ok)
	res, err := f.call(t, testsvc.DeleteThing)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Deprecation"); got != "@1767225600" {
		t.Errorf("Deprecation = %q", got)
	}
	if got := res.Header().Get("Sunset"); got != "Fri, 01 Jan 2027 00:00:00 GMT" {
		t.Errorf("Sunset = %q", got)
	}
}

func errWithReason(t *testing.T, code connect.Code, reason string) handlerFunc {
	t.Helper()
	return func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
		cerr := connect.NewError(code, errors.New("failed"))
		detail, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		cerr.AddDetail(detail)
		return nil, cerr
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
		{"no detail", func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("failed"))
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
