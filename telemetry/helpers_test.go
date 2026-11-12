package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"
	"connectrpc.com/connect/v2/connectproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/telemetry"
)

// handlerFunc is a unary handler of the test service.
type handlerFunc func(context.Context) error

func ok(context.Context) error { return nil }

// fails returns a handler that fails with code and, when reason is set, an
// ErrorInfo detail carrying it.
func fails(code connect.Code, reason string) handlerFunc {
	return func(context.Context) error {
		err := connect.NewError(code, "it failed")
		if reason == "" {
			return err
		}
		detail, derr := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason})
		if derr != nil {
			return derr
		}
		return err.WithDetail(detail)
	}
}

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
	return connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary}
}

// signals records what the instrumentation emits.
type signals struct {
	spans   *tracetest.SpanRecorder
	tracer  *sdktrace.TracerProvider
	reader  *sdkmetric.ManualReader
	meter   *sdkmetric.MeterProvider
	propag  propagation.TextMapPropagator
	cfg     telemetry.Config
	t       *testing.T
	baggage baggage.Baggage // the baggage the last handler saw
}

func newSignals(t *testing.T) *signals {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	propag := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	return &signals{
		spans: spans, tracer: tracer, reader: reader, meter: meter, propag: propag, t: t,
		cfg: telemetry.Config{TracerProvider: tracer, MeterProvider: meter, Propagator: propag},
	}
}

// server returns a connect.Server whose test-service methods run h behind
// flow IDs and the server interceptor.
func (s *signals) server(h handlerFunc) *connect.Server {
	s.t.Helper()
	observe, err := telemetry.NewServerInterceptor(s.cfg)
	if err != nil {
		s.t.Fatal(err)
	}
	server := connect.NewServer(flowid.Interceptor(), observe)
	record := func(ctx context.Context) error {
		s.baggage = baggage.FromContext(ctx)
		return h(ctx)
	}
	for _, p := range []string{testsvc.GetThing, testsvc.DeleteThing} {
		server.Register(connect.Method{Spec: spec(p), Handler: unary(record)})
	}
	return server
}

// serve serves the server over HTTP and returns its URL.
func (s *signals) serve(server *connect.Server) string {
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewServer(mux)
	s.t.Cleanup(srv.Close)
	return srv.URL
}

// call calls procedure over HTTP with the given request headers.
func call(t *testing.T, url, procedure string, header map[string]string, opts ...connecthttp.Option) error {
	t.Helper()
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, url, opts...))
	ctx, info := connect.NewClientContext(t.Context())
	for k, v := range header {
		info.RequestHeader().Set(k, v)
	}
	return client.CallUnary(ctx, spec(procedure), &emptypb.Empty{}, &emptypb.Empty{})
}

// inProcess calls procedure on server in-process with ctx.
func inProcess(ctx context.Context, server *connect.Server, procedure string) error {
	client := connect.NewClient(connectinprocess.New(server))
	return client.CallUnary(ctx, spec(procedure), &emptypb.Empty{}, &emptypb.Empty{})
}

// ended returns the ended spans.
func (s *signals) ended() []sdktrace.ReadOnlySpan {
	return s.spans.Ended()
}

// only returns the one ended span, failing otherwise.
func (s *signals) only() sdktrace.ReadOnlySpan {
	s.t.Helper()
	ended := s.ended()
	if len(ended) != 1 {
		s.t.Fatalf("got %d spans, want 1", len(ended))
	}
	return ended[0]
}

// attrs returns a span's attributes as a map.
func attrs(kvs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	out := make(map[attribute.Key]attribute.Value, len(kvs))
	for _, kv := range kvs {
		out[kv.Key] = kv.Value
	}
	return out
}

// histogram returns the data points of the named histogram.
func (s *signals) histogram(name string) []metricdata.HistogramDataPoint[float64] {
	s.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := s.reader.Collect(context.Background(), &rm); err != nil {
		s.t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if m.Unit != "s" {
				s.t.Errorf("%s unit = %q, want s", name, m.Unit)
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				s.t.Fatalf("%s is %T, want a float64 histogram", name, m.Data)
			}
			return h.DataPoints
		}
	}
	return nil
}

func attrKey(s string) attribute.Key { return attribute.Key(s) }
