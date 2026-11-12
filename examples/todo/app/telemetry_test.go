package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/raveesh-me/dedo/examples/todo/app"
	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/dedo/telemetry"
)

// The caller's trace: every transport continues it.
const (
	callerTrace  = "4bf92f3577b34da6a3ce929d0e0e4736"
	callerSpan   = "00f067aa0ba902b7"
	traceparent  = "00-" + callerTrace + "-" + callerSpan + "-01"
	secretEmail  = "secret.person@example.com"
	createMethod = "raveesh_me.todo.v1.TodoService/CreateTodo"
)

type observed struct {
	env
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
}

func newObserved(t *testing.T, enforce bool) observed {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	var log bytes.Buffer
	handler, err := app.NewHandler(app.Config{
		Log:           slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&log, nil))),
		EnforceScopes: enforce,
		Telemetry: telemetry.Config{
			TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)),
			MeterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
			Propagator:     propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return observed{env: env{url: srv.URL, log: &log}, spans: spans, reader: reader}
}

// withHeaders sets fixed headers on every request.
type withHeaders map[string]string

func (h withHeaders) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range h {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func traced(token string) withHeaders {
	return withHeaders{"Authorization": "Bearer " + token, "traceparent": traceparent}
}

// rpcSpan returns the one ended span of method, failing otherwise.
func (o observed) rpcSpan(t *testing.T, method string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range o.spans.Ended() {
		if s.Name() == method {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d spans named %s, want 1", len(found), method)
	}
	return found[0]
}

func attr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit()
		}
	}
	return ""
}

func TestTraceContinuesOverConnect(t *testing.T) {
	o := newObserved(t, true)
	client := todov1connect.NewTodoServiceClient(connect.NewClient(connecthttp.NewTransport(
		&http.Client{Transport: traced(app.WriterToken)}, o.url)))
	if _, err := client.CreateTodo(t.Context(), &todov1.CreateTodoRequest{
		TodoId: "c", Todo: &todov1.Todo{Title: "t", AssigneeEmail: secretEmail},
	}); err != nil {
		t.Fatal(err)
	}
	span := o.rpcSpan(t, createMethod)
	if span.SpanContext().TraceID().String() != callerTrace || span.Parent().SpanID().String() != callerSpan {
		t.Errorf("span is not in the caller's trace: %v", span.Parent())
	}
	if got := attr(span, "dedo.rpc.transport"); got != "connect" {
		t.Errorf("transport = %q", got)
	}
	// The contract's attributes: the sensitive email by its presence only.
	if got := attr(span, "todo.assigned"); got != "true" {
		t.Errorf("todo.assigned = %q, want true", got)
	}
	if got := attr(span, "todo.done"); got != "false" {
		t.Errorf("todo.done = %q, want false", got)
	}
	o.assertNoSecret(t)
}

func TestTraceContinuesOverREST(t *testing.T) {
	o := newObserved(t, true)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, o.url+"/commerce/todo/v1/todos?todo_id=r",
		strings.NewReader(`{"title":"t","assignee_email":"`+secretEmail+`"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Transport: traced(app.WriterToken)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	span := o.rpcSpan(t, createMethod)
	if span.SpanContext().TraceID().String() != callerTrace || span.Parent().SpanID().String() != callerSpan {
		t.Errorf("span is not in the caller's trace: %v", span.Parent())
	}
	if got := attr(span, "dedo.rpc.transport"); got != "rest" {
		t.Errorf("transport = %q", got)
	}
	o.assertNoSecret(t)
}

func TestTraceContinuesOverMCP(t *testing.T) {
	o := newObserved(t, true)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{Endpoint: o.url + "/mcp", HTTPClient: &http.Client{Transport: traced(app.WriterToken)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: createTool, Arguments: map[string]any{
		"todo_id": "m", "todo": map[string]any{"title": "t", "assignee_email": secretEmail},
	}})
	if err != nil || res.IsError {
		t.Fatalf("create via MCP: %v %s", err, text(res))
	}
	span := o.rpcSpan(t, createMethod)
	if got := attr(span, "dedo.rpc.transport"); got != "mcp" {
		t.Errorf("transport = %q", got)
	}
	var parent sdktrace.ReadOnlySpan
	for _, s := range o.spans.Ended() {
		if s.SpanContext().SpanID() == span.Parent().SpanID() {
			parent = s
		}
	}
	if parent == nil || parent.Name() != "POST /mcp" {
		t.Fatalf("the tool call's parent is %v, want the MCP request's span", parent)
	}
	if parent.SpanContext().TraceID().String() != callerTrace || parent.Parent().SpanID().String() != callerSpan {
		t.Errorf("the MCP request's span is not in the caller's trace: %v", parent.Parent())
	}
	o.assertNoSecret(t)
}

// Log records written while serving a call carry its trace.
func TestLogsCarryTheTrace(t *testing.T) {
	o := newObserved(t, false)
	client := todov1connect.NewTodoServiceClient(connect.NewClient(connecthttp.NewTransport(
		&http.Client{Transport: withHeaders{"traceparent": traceparent}}, o.url)))
	// No token, in audit mode: the policy interceptor logs the denial.
	if _, err := client.ListTodos(t.Context(), &todov1.ListTodosRequest{}); err != nil {
		t.Fatal(err)
	}
	span := o.rpcSpan(t, "raveesh_me.todo.v1.TodoService/ListTodos")
	var found bool
	for _, line := range bytes.Split(bytes.TrimSpace(o.log.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if rec["trace_id"] == callerTrace && rec["span_id"] == span.SpanContext().SpanID().String() {
			found = true
		}
	}
	if !found {
		t.Errorf("no log record carries the call's trace_id and span_id: %s", o.log.String())
	}
}

// assertNoSecret fails when the sensitive field's value reached a span, a
// metric or a log record.
func (o observed) assertNoSecret(t *testing.T) {
	t.Helper()
	for _, s := range o.spans.Ended() {
		for _, kv := range s.Attributes() {
			if strings.Contains(kv.Value.Emit(), secretEmail) {
				t.Errorf("span %s attribute %s holds the sensitive value", s.Name(), kv.Key)
			}
		}
		for _, ev := range s.Events() {
			for _, kv := range ev.Attributes {
				if strings.Contains(kv.Value.Emit(), secretEmail) {
					t.Errorf("span %s event %s holds the sensitive value", s.Name(), ev.Name)
				}
			}
		}
		if strings.Contains(s.Status().Description, secretEmail) {
			t.Errorf("span %s status holds the sensitive value", s.Name())
		}
	}
	var rm metricdata.ResourceMetrics
	if err := o.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(rm); strings.Contains(string(b), secretEmail) {
		t.Error("a metric holds the sensitive value")
	}
	if strings.Contains(o.log.String(), secretEmail) {
		t.Error("a log record holds the sensitive value")
	}
}
