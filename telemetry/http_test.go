package telemetry_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/telemetry"
)

func TestMiddlewareStartsServerSpanFromTraceparent(t *testing.T) {
	s := newSignals(t)
	server := s.server(ok)
	// The endpoint stands in for /mcp: it calls the server in-process with
	// the request's context, as a tool call does.
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := inProcess(telemetry.WithTransport(r.Context(), telemetry.TransportMCP), server, testsvc.GetThing); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(flowid.Middleware(telemetry.Middleware(s.cfg, "/mcp", endpoint)))
	t.Cleanup(srv.Close)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/mcp", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	req.Header.Set(flowid.Header, "flow-mcp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	httpSpan := findKind(s, trace.SpanKindServer)
	ended := s.ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans, want the MCP request's and the tool call's", len(ended))
	}
	rpc, mcp := ended[0], ended[1]
	if mcp != httpSpan || mcp.Name() != "POST /mcp" {
		t.Fatalf("spans = %q, %q", rpc.Name(), mcp.Name())
	}
	if mcp.SpanContext().TraceID().String() != traceID || !mcp.Parent().IsRemote() {
		t.Errorf("MCP request span is not a child of the caller: %v", mcp.Parent())
	}
	if rpc.Parent().SpanID() != mcp.SpanContext().SpanID() {
		t.Error("tool call is not a child of the MCP request")
	}
	got := attrs(mcp.Attributes())
	for k, v := range map[string]string{"http.request.method": "POST", "http.route": "/mcp", "url.path": "/mcp", "url.scheme": "http", "dedo.flow_id": "flow-mcp"} {
		if got[attrKey(k)].AsString() != v {
			t.Errorf("%s = %q, want %q", k, got[attrKey(k)].AsString(), v)
		}
	}
	if got["http.response.status_code"].AsInt64() != 200 {
		t.Errorf("http.response.status_code = %v", got["http.response.status_code"])
	}
}

func TestMiddlewareServerErrorStatus(t *testing.T) {
	for _, tt := range []struct {
		status  int
		isError bool
	}{{http.StatusOK, false}, {http.StatusNotFound, false}, {http.StatusBadGateway, true}} {
		s := newSignals(t)
		srv := httptest.NewServer(telemetry.Middleware(s.cfg, "/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
		})))
		resp, err := http.Post(srv.URL+"/mcp", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		srv.Close()
		span := s.only()
		if (span.Status().Code == codes.Error) != tt.isError {
			t.Errorf("%d: status = %v, want error %v", tt.status, span.Status(), tt.isError)
		}
		_, hasType := attrs(span.Attributes())["error.type"]
		if hasType != tt.isError {
			t.Errorf("%d: error.type set %v", tt.status, hasType)
		}
	}
}

func TestMiddlewareImplicitOK(t *testing.T) {
	s := newSignals(t)
	srv := httptest.NewServer(telemetry.Middleware(s.cfg, "/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	})))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := attrs(s.only().Attributes())["http.response.status_code"].AsInt64(); got != 200 {
		t.Errorf("status code = %d, want 200", got)
	}
}

func TestMiddlewareKeepsAnOuterSpan(t *testing.T) {
	s := newSignals(t)
	inner := telemetry.Middleware(s.cfg, "/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	var outer trace.Span
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := s.tracer.Tracer("outer").Start(r.Context(), "outer")
		outer = span
		inner.ServeHTTP(w, r.WithContext(ctx))
		span.End()
	}))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/mcp", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, sp := range s.ended() {
		if sp.Name() == "POST /mcp" && sp.Parent().SpanID() != outer.SpanContext().SpanID() {
			t.Error("the MCP request's span is not a child of the span already in the request's context")
		}
	}
}

func TestMiddlewareIgnoresInformationalStatus(t *testing.T) {
	s := newSignals(t)
	srv := httptest.NewServer(telemetry.Middleware(s.cfg, "/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusBadGateway)
	})))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	span := s.only()
	if got := attrs(span.Attributes())["http.response.status_code"].AsInt64(); got != http.StatusBadGateway {
		t.Errorf("status code = %d, want 502", got)
	}
}

func TestMiddlewareRecordsPanicAsServerError(t *testing.T) {
	s := newSignals(t)
	h := telemetry.Middleware(s.cfg, "/mcp", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not propagate")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	}()
	span := s.only()
	if span.Status().Code != codes.Error {
		t.Errorf("status = %v, want error", span.Status())
	}
	if got := attrs(span.Attributes())["http.response.status_code"].AsInt64(); got != http.StatusInternalServerError {
		t.Errorf("status code = %d, want 500", got)
	}
}
