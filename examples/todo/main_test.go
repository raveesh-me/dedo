package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// collector records the OTLP requests it receives, by path.
type collector struct {
	mu     sync.Mutex
	bodies map[string][]byte
}

func newCollector(t *testing.T) (*collector, string) {
	t.Helper()
	c := &collector{bodies: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies[r.URL.Path] = append(c.bodies[r.URL.Path], b...)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return c, srv.URL
}

func (c *collector) got(path string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bodies[path]
}

// emit records a span and a metric through the global providers, then
// shuts the SDK down, which flushes both.
func emit(t *testing.T, shutdown func(context.Context) error) {
	t.Helper()
	_, span := otel.Tracer("test").Start(t.Context(), "op")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.count")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 1)
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestSetupTelemetryExportsOverOTLP(t *testing.T) {
	c, url := newCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", url)
	t.Setenv("OTEL_SERVICE_NAME", "todo-under-test")
	shutdown, err := setupTelemetry(t.Context(), discard())
	if err != nil {
		t.Fatal(err)
	}
	emit(t, shutdown)
	traces, metrics := c.got("/v1/traces"), c.got("/v1/metrics")
	if len(traces) == 0 || len(metrics) == 0 {
		t.Fatalf("collector got %d bytes of traces and %d of metrics", len(traces), len(metrics))
	}
	if !bytes.Contains(traces, []byte("todo-under-test")) {
		t.Error("OTEL_SERVICE_NAME did not name the service")
	}
}

func TestSetupTelemetryDefaultServiceName(t *testing.T) {
	c, url := newCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", url)
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	shutdown, err := setupTelemetry(t.Context(), discard())
	if err != nil {
		t.Fatal(err)
	}
	emit(t, shutdown)
	if !bytes.Contains(c.got("/v1/traces"), []byte(serviceName)) {
		t.Errorf("service.name is not %q", serviceName)
	}
	if len(c.got("/v1/metrics")) != 0 {
		t.Error("OTEL_METRICS_EXPORTER=none exported metrics")
	}
}

func TestSetupTelemetryOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"disabled": {"OTEL_SDK_DISABLED": "true"},
		"none":     {"OTEL_TRACES_EXPORTER": "none", "OTEL_METRICS_EXPORTER": "none"},
	} {
		t.Run(name, func(t *testing.T) {
			c, url := newCollector(t)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", url)
			for k, v := range env {
				t.Setenv(k, v)
			}
			tracer, meter := otel.GetTracerProvider(), otel.GetMeterProvider()
			shutdown, err := setupTelemetry(t.Context(), discard())
			if err != nil {
				t.Fatal(err)
			}
			if otel.GetTracerProvider() != tracer || otel.GetMeterProvider() != meter {
				t.Error("installed a provider with telemetry off")
			}
			emit(t, shutdown)
			if len(c.got("/v1/traces"))+len(c.got("/v1/metrics")) != 0 {
				t.Error("exported with telemetry off")
			}
		})
	}
}

func TestSetupTelemetryRejectsUnsupportedSettings(t *testing.T) {
	for name, env := range map[string][2]string{
		"exporter":        {"OTEL_TRACES_EXPORTER", "zipkin"},
		"protocol":        {"OTEL_EXPORTER_OTLP_PROTOCOL", "grpc"},
		"signal protocol": {"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "grpc"},
		"propagator":      {"OTEL_PROPAGATORS", "b3"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(env[0], env[1])
			shutdown, err := setupTelemetry(t.Context(), discard())
			if err == nil {
				_ = shutdown(t.Context())
				t.Fatalf("%s=%s: want an error", env[0], env[1])
			}
		})
	}
}

func TestPropagatorFromEnv(t *testing.T) {
	for value, fields := range map[string]int{"": 3, "tracecontext": 2, "baggage": 1, "none": 0, "tracecontext, baggage": 3} {
		p, err := propagatorFromEnv(value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if got := len(p.Fields()); got != fields {
			t.Errorf("%q: %d fields %v, want %d", value, got, p.Fields(), fields)
		}
	}
}

// With the SDK disabled, the caller's trace context still propagates.
func TestSetupTelemetryDisabledStillPropagates(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "true")
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	if _, err := setupTelemetry(t.Context(), discard()); err != nil {
		t.Fatal(err)
	}
	if fields := otel.GetTextMapPropagator().Fields(); !slices.Contains(fields, "traceparent") {
		t.Errorf("propagator fields = %v, want traceparent", fields)
	}
}
