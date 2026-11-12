package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/raveesh-me/dedo/telemetry"
)

func decode(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(line, &out); err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return out
}

func TestLogHandlerAddsTraceAndSpanIDs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(t.Context(), "op")
	defer span.End()

	log.InfoContext(ctx, "hello", "k", "v")
	got := decode(t, buf.Bytes())
	sc := span.SpanContext()
	if got["trace_id"] != sc.TraceID().String() || got["span_id"] != sc.SpanID().String() {
		t.Errorf("record = %v, want trace_id %s span_id %s", got, sc.TraceID(), sc.SpanID())
	}
	hex := regexp.MustCompile(`^[0-9a-f]+$`)
	if !hex.MatchString(got["trace_id"].(string)) || len(got["trace_id"].(string)) != 32 || len(got["span_id"].(string)) != 16 {
		t.Errorf("IDs are not lowercase hex: %v", got)
	}
	if got["k"] != "v" || got["msg"] != "hello" {
		t.Errorf("record lost its own attributes: %v", got)
	}
}

func TestLogHandlerWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	log.InfoContext(t.Context(), "hello")
	log.Info("no context")
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		got := decode(t, line)
		if _, ok := got["trace_id"]; ok {
			t.Errorf("trace_id without a span: %v", got)
		}
		if _, ok := got["span_id"]; ok {
			t.Errorf("span_id without a span: %v", got)
		}
	}
}

func TestLogHandlerRemoteSpanContext(t *testing.T) {
	// A caller's trace context with no SDK installed still gives the IDs.
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
	log.InfoContext(ctx, "hello")
	got := decode(t, buf.Bytes())
	if got["trace_id"] != tid.String() || got["span_id"] != sid.String() {
		t.Errorf("record = %v", got)
	}
}

func TestLogHandlerKeepsIDsTopLevelInGroups(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(t.Context(), "op")
	defer span.End()

	log.With("a", 1).WithGroup("g").With("b", 2).WithGroup("h").InfoContext(ctx, "nested", "c", 3)
	got := decode(t, buf.Bytes())
	if got["trace_id"] != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id not top-level: %v", got)
	}
	g, _ := got["g"].(map[string]any)
	h, _ := g["h"].(map[string]any)
	if got["a"] != float64(1) || g["b"] != float64(2) || h["c"] != float64(3) {
		t.Errorf("groups not preserved: %v", got)
	}
	if _, ok := g["trace_id"]; ok {
		t.Errorf("trace_id inside a group: %v", got)
	}
}

func TestLogHandlerEnabledDelegates(t *testing.T) {
	h := telemetry.NewLogHandler(slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("info enabled under a warn handler")
	}
	if !h.Enabled(t.Context(), slog.LevelError) {
		t.Error("error disabled")
	}
	if h.WithGroup("") != h {
		t.Error("WithGroup(\"\") must return the handler itself")
	}
}
