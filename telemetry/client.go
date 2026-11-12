package telemetry

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// NewClientInterceptor returns a client interceptor that gives every
// outbound call a client span, records rpc.client.call.duration, and
// injects the trace context and baggage into the request headers
// (traceparent, tracestate, baggage), as flowid.ClientInterceptor does for
// the flow ID. A client span counts every code but OK as an error.
func NewClientInterceptor(cfg Config) (connect.ClientInterceptor, error) {
	cfg = cfg.withDefaults()
	duration, err := newDuration(cfg.meter(), clientDuration, "Duration of an outgoing RPC.")
	if err != nil {
		return nil, err
	}
	c := &client{cfg: cfg, tracer: cfg.tracer(), duration: duration}
	return c.wrap, nil
}

type client struct {
	cfg      Config
	tracer   trace.Tracer
	duration metric.Float64Histogram
}

func (c *client) wrap(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		info, ok := connect.CallInfoForClientContext(ctx)
		if !ok {
			ctx, info = connect.NewClientContext(ctx)
		}
		method := strings.TrimPrefix(spec.Procedure, "/")
		ctx, span := c.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrRPCSystem.String(rpcSystem), attrRPCMethod.String(method)))
		c.cfg.Propagator.Inject(ctx, headerCarrier{info.RequestHeader()})
		call := &clientCall{ctx: ctx, client: c, method: method, info: info, span: span, start: time.Now()}
		stream, err := next(ctx, spec)
		if err != nil {
			call.fail(err)
			call.finish()
			return nil, err
		}
		call.ClientStream = stream
		// A stream the caller abandons without reading to the end ends with
		// its context, as an error.
		if spec.StreamType != connect.StreamTypeUnary && ctx.Done() != nil {
			call.mu.Lock()
			call.stop = context.AfterFunc(ctx, call.cancelled)
			call.mu.Unlock()
		}
		return call, nil
	}
}

// clientCall is one outbound call's stream. The call ends on the first
// Receive error (io.EOF included), on Close, or when its context is done.
type clientCall struct {
	connect.ClientStream

	ctx    context.Context //nolint:containedctx // the call's, for recording its end
	client *client
	method string
	info   *connect.CallInfo
	span   trace.Span
	start  time.Time

	once sync.Once
	mu   sync.Mutex // guards stop and err
	stop func() bool
	err  error
}

func (c *clientCall) SendHeaders() error { return c.fail(c.ClientStream.SendHeaders()) }
func (c *clientCall) Send(msg any) error { return c.fail(c.ClientStream.Send(msg)) }
func (c *clientCall) CloseSend() error   { return c.fail(c.ClientStream.CloseSend()) }

func (c *clientCall) Receive(msg any) error {
	err := c.ClientStream.Receive(msg)
	if err != nil {
		c.fail(err)
		c.finish()
	}
	return err
}

func (c *clientCall) Close() error {
	err := c.ClientStream.Close()
	c.finish()
	return err
}

// fail remembers err as the call's outcome, unless it is nil or io.EOF,
// and returns it.
func (c *clientCall) fail(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	return err
}

// cancelled ends the call when its context is done: canceled, or past its
// deadline, unless it already failed otherwise.
func (c *clientCall) cancelled() {
	cause := c.ctx.Err()
	code := connect.CodeCanceled
	if errors.Is(cause, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = connect.NewError(code, cause.Error())
	}
	c.mu.Unlock()
	c.finish()
}

// finish ends the call once.
func (c *clientCall) finish() {
	c.mu.Lock()
	stop := c.stop
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	c.once.Do(c.end)
}

func (c *clientCall) end() {
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	transport, _ := transportOf(context.Background(), c.info.Protocol)
	base := slices.Concat(rpcAttributes(c.method, transport),
		addressAttributes(c.info.PeerAddr, attrServerAddress, attrServerPort))
	o := outcomeOf(err)
	isErr := clientIsError(c.method, o)
	status := statusAttributes(o, isErr)
	c.span.SetAttributes(slices.Concat(base, status)...)
	c.span.SetStatus(spanStatus(o, isErr))
	c.client.duration.Record(c.ctx, time.Since(c.start).Seconds(),
		metric.WithAttributes(slices.Concat(base, status)...))
	c.span.End()
}
