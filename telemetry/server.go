package telemetry

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/raveesh-me/dedo/flowid"
)

// NewServerInterceptor returns a server interceptor that gives every RPC a
// server span and records rpc.server.call.duration, on every transport that
// reaches the server: Connect, gRPC, gRPC-Web, REST through Vanguard, and
// MCP tool calls made in-process.
//
// It continues the caller's trace from the traceparent and baggage request
// headers (W3C Trace Context and Baggage), unless the call's context
// already holds a span, as an in-process call's does. Put it right after
// flowid.Interceptor, so the span carries the flow ID and covers every
// other interceptor.
func NewServerInterceptor(cfg Config) (connect.ServerInterceptor, error) {
	cfg = cfg.withDefaults()
	duration, err := newDuration(cfg.meter(), serverDuration, "Duration of an incoming RPC.")
	if err != nil {
		return nil, err
	}
	s := &server{
		cfg:      cfg,
		tracer:   cfg.tracer(),
		duration: duration,
		isError:  serverIsError,
	}
	return s.wrap, nil
}

type server struct {
	cfg      Config
	tracer   trace.Tracer
	duration metric.Float64Histogram
	isError  classifier
}

func (s *server) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		start := time.Now()
		var protocol, peer string
		info, hasInfo := connect.CallInfoForServerContext(ctx)
		if hasInfo {
			protocol, peer = info.Protocol, info.PeerAddr
		}
		transport, ctx := transportOf(ctx, protocol)
		if hasInfo && protocol != "" && !trace.SpanContextFromContext(ctx).IsValid() {
			ctx = s.cfg.Propagator.Extract(ctx, headerCarrier{info.RequestHeader()})
		}
		method := strings.TrimPrefix(spec.Procedure, "/")
		base := rpcAttributes(method, transport)
		ctx, span := s.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(serverSpanAttributes(ctx, base, peer)...))
		defer span.End()
		defer func() {
			if p := recover(); p != nil {
				// Record the panic as the INTERNAL error it ends as, then
				// let it go on to whatever recovers it.
				s.record(ctx, span, spec, base, start, connect.NewError(connect.CodeInternal, "handler panicked"))
				panic(p)
			}
		}()
		err := next(ctx, spec, stream)
		s.record(ctx, span, spec, base, start, err)
		return err
	}
}

// record puts the call's outcome on its span and duration metric.
func (s *server) record(ctx context.Context, span trace.Span, spec connect.Spec, base []attribute.KeyValue, start time.Time, err error) {
	o := outcomeOf(err)
	isErr := s.isError(spec.Procedure, o)
	status := statusAttributes(o, isErr)
	span.SetAttributes(status...)
	span.SetStatus(spanStatus(o, isErr))
	s.duration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(slices.Concat(base, status)...))
}

// rpcAttributes are the attributes every span and metric of an RPC has.
func rpcAttributes(method string, transport Transport) []attribute.KeyValue {
	return []attribute.KeyValue{
		attrRPCSystem.String(rpcSystem),
		attrRPCMethod.String(method),
		AttrTransport.String(string(transport)),
	}
}

// serverSpanAttributes adds the span-only attributes to base: the peer's
// address and the flow ID, whose values are too many for a metric.
func serverSpanAttributes(ctx context.Context, base []attribute.KeyValue, peer string) []attribute.KeyValue {
	out := slices.Concat(base, addressAttributes(peer, attrPeerAddress, attrPeerPort))
	if id := flowid.From(ctx); id != "" {
		out = append(out, AttrFlowID.String(id))
	}
	return out
}
