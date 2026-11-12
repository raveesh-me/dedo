package telemetry

import (
	"context"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/trace"
)

// The log record keys of the trace and span IDs. Log pipelines join logs
// to traces on trace_id.
const (
	LogKeyTraceID = "trace_id"
	LogKeySpanID  = "span_id"
)

// NewLogHandler returns a handler that adds trace_id and span_id, as
// lowercase hex, to every record whose context holds a span, and hands it
// to next. Within an RPC whose request fields declare attributes
// ((dedo.v1.field).attribute), it adds those too. These keys stay at the
// top level of the record, outside any group the logger opened. Log with
// the context (log.InfoContext(ctx, ...)) for them to appear.
func NewLogHandler(next slog.Handler) slog.Handler {
	return &logHandler{next: next}
}

// logHandler applies the attributes and groups added before the first
// group to next directly, and keeps the later ones in groups, so that it
// can put the IDs outside them.
type logHandler struct {
	next   slog.Handler
	groups []logGroup
}

// logGroup is a group the logger opened, and the attributes added in it.
type logGroup struct {
	name  string
	attrs []slog.Attr
}

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	if len(h.groups) == 0 {
		return &logHandler{next: h.next.WithAttrs(attrs)}
	}
	groups := slices.Clone(h.groups)
	last := groups[len(groups)-1]
	groups[len(groups)-1] = logGroup{name: last.name, attrs: slices.Concat(last.attrs, attrs)}
	return &logHandler{next: h.next, groups: groups}
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &logHandler{next: h.next, groups: append(slices.Clone(h.groups), logGroup{name: name})}
}

func (h *logHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	request := requestAttrsFrom(ctx)
	if !sc.IsValid() && len(request) == 0 && len(h.groups) == 0 {
		return h.next.Handle(ctx, r)
	}
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	if sc.IsValid() {
		out.AddAttrs(slog.String(LogKeyTraceID, sc.TraceID().String()), slog.String(LogKeySpanID, sc.SpanID().String()))
	}
	for _, kv := range request {
		out.AddAttrs(slog.Any(string(kv.Key), kv.Value.AsInterface()))
	}
	out.AddAttrs(h.nest(r)...)
	return h.next.Handle(ctx, out)
}

// nest returns the record's attributes inside the open groups.
func (h *logHandler) nest(r slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	for i := len(h.groups) - 1; i >= 0; i-- {
		g := h.groups[i]
		attrs = []slog.Attr{slog.Attr{Key: g.name, Value: slog.GroupValue(slices.Concat(g.attrs, attrs)...)}}
	}
	return attrs
}
