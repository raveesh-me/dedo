package telemetry_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/telemetry"
)

// client returns a client of url behind the client interceptor.
func (s *signals) client(url string) *connect.Client {
	s.t.Helper()
	observe, err := telemetry.NewClientInterceptor(s.cfg)
	if err != nil {
		s.t.Fatal(err)
	}
	return connect.NewClient(connecthttp.NewTransport(http.DefaultClient, url), observe)
}

func TestClientSpanAndPropagation(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(ok))
	ctx, parent := s.tracer.Tracer("test").Start(t.Context(), "handler")
	err := s.client(url).CallUnary(ctx, spec(testsvc.GetThing), &emptypb.Empty{}, &emptypb.Empty{})
	parent.End()
	if err != nil {
		t.Fatal(err)
	}
	clientSpan, serverSpan := findKind(s, trace.SpanKindClient), findKind(s, trace.SpanKindServer)
	if clientSpan == nil || serverSpan == nil {
		t.Fatalf("spans = %d, want a client and a server span", len(s.ended()))
	}
	if clientSpan.Name() != method {
		t.Errorf("client span name = %q", clientSpan.Name())
	}
	if clientSpan.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("client span is not a child of the caller's span")
	}
	if serverSpan.Parent().SpanID() != clientSpan.SpanContext().SpanID() || !serverSpan.Parent().IsRemote() {
		t.Error("server span is not a remote child of the client span: traceparent was not injected")
	}
	got := attrs(clientSpan.Attributes())
	if got["rpc.system.name"].AsString() != "connectrpc" || got["rpc.method"].AsString() != method {
		t.Errorf("client attributes = %v", clientSpan.Attributes())
	}
	if got["server.address"].AsString() != "127.0.0.1" {
		t.Errorf("server.address = %q", got["server.address"].AsString())
	}
	if got["server.port"].AsInt64() == 0 {
		t.Error("no server.port")
	}
	if got["rpc.status_code"].AsString() != "OK" {
		t.Errorf("rpc.status_code = %q", got["rpc.status_code"].AsString())
	}
	points := s.histogram("rpc.client.call.duration")
	if len(points) != 1 || points[0].Count != 1 {
		t.Fatalf("client duration points = %v", points)
	}
	if v, _ := points[0].Attributes.Value("server.address"); v.AsString() != "127.0.0.1" {
		t.Errorf("metric server.address = %q", v.AsString())
	}
}

func TestClientStatusEveryNonOKCodeIsError(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeNotFound, connect.CodeInvalidArgument, connect.CodeInternal} {
		t.Run(code.String(), func(t *testing.T) {
			s := newSignals(t)
			url := s.serve(s.server(fails(code, testsvc.Reason)))
			err := s.client(url).CallUnary(t.Context(), spec(testsvc.GetThing), &emptypb.Empty{}, &emptypb.Empty{})
			if connect.CodeOf(err) != code {
				t.Fatalf("err = %v", err)
			}
			span := findKind(s, trace.SpanKindClient)
			if span == nil {
				t.Fatal("no client span")
			}
			if span.Status().Code != codes.Error {
				t.Errorf("status = %v, want error", span.Status())
			}
			got := attrs(span.Attributes())
			upper := strings.ToUpper(code.String())
			if got["error.type"].AsString() != upper || got["rpc.status_code"].AsString() != upper {
				t.Errorf("error.type = %q, rpc.status_code = %q, want %q", got["error.type"].AsString(), got["rpc.status_code"].AsString(), upper)
			}
			if got["dedo.error.reason"].AsString() != testsvc.Reason {
				t.Errorf("dedo.error.reason = %q", got["dedo.error.reason"].AsString())
			}
		})
	}
}

func TestClientTransportFailure(t *testing.T) {
	s := newSignals(t)
	// Nothing listens here.
	err := s.client("http://127.0.0.1:1").CallUnary(t.Context(), spec(testsvc.GetThing), &emptypb.Empty{}, &emptypb.Empty{})
	if err == nil {
		t.Fatal("want an error")
	}
	span := findKind(s, trace.SpanKindClient)
	if span == nil {
		t.Fatal("no client span")
	}
	if span.Status().Code != codes.Error {
		t.Errorf("status = %v", span.Status())
	}
	if got := attrs(span.Attributes())["error.type"].AsString(); got != "UNAVAILABLE" {
		t.Errorf("error.type = %q, want UNAVAILABLE", got)
	}
}

func TestClientStreamAbandonedEndsSpan(t *testing.T) {
	s := newSignals(t)
	url := s.serve(s.server(ok))
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := s.client(url).CallClientStream(ctx, connect.Spec{Procedure: testsvc.GetThing, StreamType: connect.StreamTypeUnary})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if findKind(s, trace.SpanKindClient) == nil {
		t.Fatal("closing the stream did not end the client span")
	}
	// Closing twice ends it once.
	_ = stream.Close()
	n := 0
	for _, sp := range s.ended() {
		if sp.SpanKind() == trace.SpanKindClient {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d client spans, want 1", n)
	}
}

// findKind returns the last ended span of a kind, or nil.
func findKind(s *signals, kind trace.SpanKind) sdktrace.ReadOnlySpan {
	var found sdktrace.ReadOnlySpan
	for _, sp := range s.ended() {
		if sp.SpanKind() == kind {
			found = sp
		}
	}
	return found
}

// A stream whose context ends before the caller reads it to the end or
// closes it is an error on the client span.
func TestClientStreamCancelledIsError(t *testing.T) {
	const watch = "/raveesh_me.test.v1.TestService/WatchThings"
	for _, tt := range []struct {
		name   string
		cancel func(context.Context) (context.Context, context.CancelFunc)
		want   string
	}{
		{"canceled", context.WithCancel, "CANCELED"},
		{"deadline", func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, 50*time.Millisecond)
		}, "DEADLINE_EXCEEDED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSignals(t)
			server := connect.NewServer()
			server.Register(connect.Method{
				Spec: connect.Spec{Procedure: watch, StreamType: connect.StreamTypeServer},
				Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
					if err := stream.Receive(&emptypb.Empty{}); err != nil {
						return err
					}
					if err := stream.Send(&emptypb.Empty{}); err != nil {
						return err
					}
					<-ctx.Done()
					return ctx.Err()
				},
			})
			url := s.serve(server)
			ctx, cancel := tt.cancel(t.Context())
			defer cancel()
			stream, err := s.client(url).CallServerStream(ctx, connect.Spec{Procedure: watch, StreamType: connect.StreamTypeServer}, &emptypb.Empty{})
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				t.Fatal(err)
			}
			if tt.want == "CANCELED" {
				cancel()
			}
			// The caller abandons the stream: its context ends the span.
			var span sdktrace.ReadOnlySpan
			for deadline := time.Now().Add(5 * time.Second); span == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				span = findKind(s, trace.SpanKindClient)
			}
			if span == nil {
				t.Fatal("no client span")
			}
			if span.Status().Code != codes.Error {
				t.Errorf("status = %v, want error", span.Status())
			}
			if got := attrs(span.Attributes())["error.type"].AsString(); got != tt.want {
				t.Errorf("error.type = %q, want %q", got, tt.want)
			}
			_ = stream.Close()
		})
	}
}
