package telemetry

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/raveesh-me/dedo/flowid"
)

// Middleware gives each request to a plain HTTP route, such as /mcp, a
// server span named "<method> <route>" (the semantic conventions for HTTP
// servers), continuing the caller's trace from traceparent and baggage.
// The RPCs a handler makes in-process with the request's context, as MCP
// tool calls do, are children of that span, not new traces.
//
// Wrap it outside the handler and inside flowid.Middleware:
//
//	mux.Handle("/mcp", flowid.Middleware(telemetry.Middleware(cfg, "/mcp", endpoint)))
func Middleware(cfg Config, route string, next http.Handler) http.Handler {
	cfg = cfg.withDefaults()
	tracer := cfg.tracer()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// A span already in the request's context, such as an outer HTTP
		// instrumentation's, is the parent; otherwise the caller's is.
		if !trace.SpanContextFromContext(ctx).IsValid() {
			ctx = cfg.Propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		attrs := []attribute.KeyValue{
			attrHTTPMethod.String(r.Method),
			attrHTTPRoute.String(route),
			attrURLPath.String(r.URL.Path),
			attrURLScheme.String(scheme),
		}
		if id := flowid.From(ctx); id != "" {
			attrs = append(attrs, AttrFlowID.String(id))
		}
		ctx, span := tracer.Start(ctx, r.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
		defer span.End()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				// The handler panicked: net/http answers 500, or drops the
				// connection. Record that, then let the panic go on.
				endHTTPSpan(span, http.StatusInternalServerError)
				panic(p)
			}
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
		endHTTPSpan(span, sw.code())
	})
}

// endHTTPSpan records the response's status code on the span. A server
// span is an error for a 5xx only: a 4xx is the caller's.
func endHTTPSpan(span trace.Span, code int) {
	span.SetAttributes(attrHTTPStatusCode.Int(code))
	if code >= http.StatusInternalServerError {
		span.SetAttributes(attrErrorType.String(strconv.Itoa(code)))
		span.SetStatus(codes.Error, "")
	}
}

// statusWriter remembers the status code the handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader remembers the final status: not an informational 1xx, such
// as 103 Early Hints, but 101 Switching Protocols, which is final.
func (w *statusWriter) WriteHeader(code int) {
	informational := code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
	if w.status == 0 && !informational {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the writer's Flush and
// deadlines, which a streaming MCP response needs.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Flush flushes the response, for handlers that check for http.Flusher.
func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
