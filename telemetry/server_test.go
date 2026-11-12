package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/telemetry"
)

const method = "raveesh_me.test.v1.TestService/GetThing"

func TestServerSpanFollowsSemanticConventions(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(ok))
	if err := call(t, url, testsvc.GetThing, map[string]string{"X-Flow-ID": "flow-1"}); err != nil {
		t.Fatal(err)
	}
	span := s.only()
	if span.Name() != method {
		t.Errorf("name = %q, want %q", span.Name(), method)
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("kind = %v, want server", span.SpanKind())
	}
	if span.Status().Code != codes.Unset {
		t.Errorf("status = %v, want unset", span.Status())
	}
	got := attrs(span.Attributes())
	want := map[attribute.Key]string{
		"rpc.system.name":      "connectrpc",
		"rpc.method":           method,
		"rpc.status_code":      "OK",
		"dedo.rpc.transport":   "connect",
		"dedo.flow_id":         "flow-1",
		"network.peer.address": "127.0.0.1",
	}
	for k, v := range want {
		if got[k].Emit() != v {
			t.Errorf("%s = %q, want %q", k, got[k].Emit(), v)
		}
	}
	if _, ok := got["network.peer.port"]; !ok {
		t.Error("no network.peer.port")
	}
	for _, k := range []attribute.Key{"error.type", "dedo.error.reason"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s set on a successful call", k)
		}
	}
	if span.InstrumentationScope().Name != "github.com/raveesh-me/dedo/telemetry" {
		t.Errorf("scope = %q", span.InstrumentationScope().Name)
	}
}

func TestServerStatusMapping(t *testing.T) {
	// A server span is an error only for codes that mean the server failed;
	// the caller's mistakes are not the server's errors.
	serverErrors := map[connect.Code]bool{
		connect.CodeUnknown: true, connect.CodeDeadlineExceeded: true, connect.CodeUnimplemented: true,
		connect.CodeInternal: true, connect.CodeUnavailable: true, connect.CodeDataLoss: true,
	}
	for code := connect.CodeCanceled; code <= connect.CodeUnauthenticated; code++ {
		t.Run(code.String(), func(t *testing.T) {
			s := newSignals(t)
			url := s.serve(s.server(fails(code, "")))
			if err := call(t, url, testsvc.GetThing, nil); connect.CodeOf(err) != code {
				t.Fatalf("err = %v, want %v", err, code)
			}
			span := s.only()
			got := attrs(span.Attributes())
			upper := strings.ToUpper(code.String())
			if got["rpc.status_code"].AsString() != upper {
				t.Errorf("rpc.status_code = %q, want %q", got["rpc.status_code"].AsString(), upper)
			}
			isErr := serverErrors[code]
			if (span.Status().Code == codes.Error) != isErr {
				t.Errorf("status = %v, want error %v", span.Status(), isErr)
			}
			errType, hasType := got["error.type"]
			if hasType != isErr || (isErr && errType.AsString() != upper) {
				t.Errorf("error.type = %q (set %v), want %q only on errors", errType.AsString(), hasType, upper)
			}
		})
	}
}

func TestServerRecordsDeclaredReason(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(fails(connect.CodeNotFound, testsvc.Reason)))
	_ = call(t, url, testsvc.GetThing, nil)
	got := attrs(s.only().Attributes())
	if got["dedo.error.reason"].AsString() != testsvc.Reason {
		t.Errorf("dedo.error.reason = %q, want %q", got["dedo.error.reason"].AsString(), testsvc.Reason)
	}
	points := s.histogram("rpc.server.call.duration")
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	if v, _ := points[0].Attributes.Value("dedo.error.reason"); v.AsString() != testsvc.Reason {
		t.Errorf("metric dedo.error.reason = %q", v.AsString())
	}
}

func TestServerDurationMetric(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(fails(connect.CodeInternal, "")))
	_ = call(t, url, testsvc.GetThing, map[string]string{"X-Flow-ID": "flow-1"})
	points := s.histogram("rpc.server.call.duration")
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	p := points[0]
	if p.Count != 1 {
		t.Errorf("count = %d", p.Count)
	}
	want := map[attribute.Key]string{
		"rpc.system.name":    "connectrpc",
		"rpc.method":         method,
		"rpc.status_code":    "INTERNAL",
		"error.type":         "INTERNAL",
		"dedo.rpc.transport": "connect",
	}
	if p.Attributes.Len() != len(want) {
		t.Errorf("attributes = %v, want exactly %v", p.Attributes.ToSlice(), want)
	}
	for k, v := range want {
		if got, _ := p.Attributes.Value(k); got.Emit() != v {
			t.Errorf("%s = %q, want %q", k, got.Emit(), v)
		}
	}
	// Semantic conventions' advised boundaries, in seconds.
	if len(p.Bounds) != 18 || p.Bounds[0] != 0.0001 || p.Bounds[17] != 60 {
		t.Errorf("bounds = %v", p.Bounds)
	}
}

func TestServerExtractsTraceContextAndBaggage(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(ok))
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentID = "00f067aa0ba902b7"
	header := map[string]string{
		"traceparent": "00-" + traceID + "-" + parentID + "-01",
		"baggage":     "tenant=acme",
	}
	if err := call(t, url, testsvc.GetThing, header); err != nil {
		t.Fatal(err)
	}
	span := s.only()
	if span.SpanContext().TraceID().String() != traceID {
		t.Errorf("trace ID = %s, want %s", span.SpanContext().TraceID(), traceID)
	}
	if span.Parent().SpanID().String() != parentID || !span.Parent().IsRemote() {
		t.Errorf("parent = %v, want remote %s", span.Parent(), parentID)
	}
	if got := s.baggage.Member("tenant").Value(); got != "acme" {
		t.Errorf("handler baggage tenant = %q, want acme", got)
	}
}

func TestServerTransports(t *testing.T) {
	s := newSignals(t)
	server := s.server(ok)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	tests := []struct {
		name string
		opt  connecthttp.Option
		want string
	}{
		{"grpc", connecthttp.WithGRPC(), "grpc"},
		{"grpc-web", connecthttp.WithGRPCWeb(), "grpc_web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := connect.NewClient(connecthttp.NewTransport(srv.Client(), srv.URL, tt.opt))
			if err := client.CallUnary(t.Context(), spec(testsvc.GetThing), &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
				t.Fatal(err)
			}
			ended := s.ended()
			last := ended[len(ended)-1]
			if got := attrs(last.Attributes())["dedo.rpc.transport"].AsString(); got != tt.want {
				t.Errorf("transport = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerInProcessTransports(t *testing.T) {
	s := newSignals(t)
	server := s.server(ok)
	if err := inProcess(t.Context(), server, testsvc.GetThing); err != nil {
		t.Fatal(err)
	}
	if err := inProcess(telemetry.WithTransport(t.Context(), telemetry.TransportMCP), server, testsvc.GetThing); err != nil {
		t.Fatal(err)
	}
	ended := s.ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans", len(ended))
	}
	for i, want := range []string{"in_process", "mcp"} {
		if got := attrs(ended[i].Attributes())["dedo.rpc.transport"].AsString(); got != want {
			t.Errorf("span %d transport = %q, want %q", i, got, want)
		}
	}
}

func TestServerInProcessCallIsChildOfCallersSpan(t *testing.T) {
	s := newSignals(t)
	server := s.server(ok)
	ctx, parent := s.tracer.Tracer("test").Start(t.Context(), "POST /mcp")
	err := inProcess(telemetry.WithTransport(ctx, telemetry.TransportMCP), server, testsvc.GetThing)
	parent.End()
	if err != nil {
		t.Fatal(err)
	}
	ended := s.ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans, want the caller's and the RPC's", len(ended))
	}
	rpc := ended[0]
	if rpc.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Errorf("RPC span parent = %s, want %s", rpc.Parent().SpanID(), parent.SpanContext().SpanID())
	}
}

func TestTransportMarkerDoesNotLeakIntoHandler(t *testing.T) {
	s := newSignals(t)
	inner := s.server(ok)
	// The outer handler calls another server in-process with the context
	// it was served with; that call is not an MCP call.
	outer := s.server(func(ctx context.Context) error {
		return inProcess(ctx, inner, testsvc.GetThing)
	})
	if err := inProcess(telemetry.WithTransport(t.Context(), telemetry.TransportMCP), outer, testsvc.DeleteThing); err != nil {
		t.Fatal(err)
	}
	ended := s.ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans", len(ended))
	}
	if got := attrs(ended[0].Attributes())["dedo.rpc.transport"].AsString(); got != "in_process" {
		t.Errorf("nested call transport = %q, want in_process", got)
	}
}

func TestServerUsesGlobalsByDefault(t *testing.T) {
	observe, err := telemetry.NewServerInterceptor(telemetry.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if observe == nil {
		t.Fatal("nil interceptor")
	}
	if _, err := telemetry.NewClientInterceptor(telemetry.Config{}); err != nil {
		t.Fatal(err)
	}
}

func TestServerWithoutSDKKeepsCallersTraceID(t *testing.T) {
	// With no SDK installed, spans are no-ops, but the caller's trace
	// context still reaches the handler, so its logs can carry the trace ID.
	var seen trace.SpanContext
	observe, err := telemetry.NewServerInterceptor(telemetry.Config{Propagator: propagation.TraceContext{}})
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer(observe)
	server.Register(connect.Method{Spec: spec(testsvc.GetThing), Handler: unary(func(ctx context.Context) error {
		seen = trace.SpanContextFromContext(ctx)
		return nil
	})})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	if err := call(t, srv.URL, testsvc.GetThing, map[string]string{"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01"}); err != nil {
		t.Fatal(err)
	}
	if seen.TraceID().String() != traceID {
		t.Errorf("handler trace ID = %s, want %s", seen.TraceID(), traceID)
	}
}

func TestServerRecordsPanicAsInternal(t *testing.T) {
	s := newSignals(t)
	observe, err := telemetry.NewServerInterceptor(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	panicking := observe(func(context.Context, connect.Spec, connect.ServerStream) error { panic("boom") })
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not propagate")
			}
		}()
		_ = panicking(t.Context(), spec(testsvc.GetThing), nil)
	}()
	span := s.only()
	if span.Status().Code != codes.Error || attrs(span.Attributes())["error.type"].AsString() != "INTERNAL" {
		t.Errorf("status = %v, attributes = %v", span.Status(), span.Attributes())
	}
	if points := s.histogram("rpc.server.call.duration"); len(points) != 1 {
		t.Errorf("got %d duration points, want 1", len(points))
	}
}
