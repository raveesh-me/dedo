package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/raveesh-me/dedo/telemetry"
)

// A tool call is an RPC on the mcp transport, and a child of the span of
// the MCP request that carried it, in the caller's trace.
func TestToolCallsJoinTheMCPRequestsTrace(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	cfg := telemetry.Config{
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)),
		Propagator:     propagation.TraceContext{},
	}
	observe, err := telemetry.NewServerInterceptor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	backend := connect.NewServer(observe)
	backend.Register(connect.Method{
		Spec: connect.Spec{Procedure: echoProcedure, StreamType: connect.StreamTypeUnary},
		Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(new(structpb.Struct)); err != nil {
				return err
			}
			return stream.Send(new(structpb.Struct))
		},
	})
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	if err := AddTools(server, connect.NewClient(connectinprocess.New(backend)), echoTool()); err != nil {
		t.Fatal(err)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	ts := httptest.NewServer(telemetry.Middleware(cfg, "/mcp", handler))
	t.Cleanup(ts.Close)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	sent := headers{"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01"}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v1"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: &http.Client{Transport: sent}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("echo: %v %s", err, text(res))
	}

	var rpc sdktrace.ReadOnlySpan
	requests := map[trace.SpanID]sdktrace.ReadOnlySpan{}
	for _, s := range spans.Ended() {
		if s.Name() == "POST /mcp" {
			requests[s.SpanContext().SpanID()] = s
		} else if s.Name() == echoProcedure[1:] {
			rpc = s
		}
	}
	if rpc == nil {
		t.Fatalf("no span for the tool call among %d spans", len(spans.Ended()))
	}
	parent, ok := requests[rpc.Parent().SpanID()]
	if !ok {
		t.Fatal("the tool call's span is not a child of an MCP request's span")
	}
	if parent.SpanContext().TraceID().String() != traceID || rpc.SpanContext().TraceID().String() != traceID {
		t.Errorf("trace IDs = %s, %s, want the caller's %s", parent.SpanContext().TraceID(), rpc.SpanContext().TraceID(), traceID)
	}
	for _, kv := range rpc.Attributes() {
		if kv.Key == telemetry.AttrTransport && kv.Value.AsString() != string(telemetry.TransportMCP) {
			t.Errorf("transport = %q, want mcp", kv.Value.AsString())
		}
	}
	if n := countRPCSpans(spans.Ended()); n != 1 {
		t.Errorf("%d spans for one tool call, want 1", n)
	}
}

func countRPCSpans(spans []sdktrace.ReadOnlySpan) int {
	n := 0
	for _, s := range spans {
		if s.Name() == echoProcedure[1:] {
			n++
		}
	}
	return n
}
