package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/policy"
	"github.com/raveesh-me/dedo/telemetry/fieldattr"
)

// maxDepth bounds how deep into nested request messages the runtime looks
// for declared attributes.
const maxDepth = 8

// contract is what one RPC declares about its telemetry.
type contract struct {
	fields []fieldAttribute
	// classified says, by reason, whether an error counts against the
	// service, overriding its code.
	classified map[string]bool
}

// fieldAttribute is one request field the contract records.
type fieldAttribute struct {
	key  attribute.Key
	mode dedov1.AttributeMode
	path []protoreflect.FieldDescriptor // from the request to the field
}

// contracts reads every RPC's telemetry contract from the table, failing
// on a declaration that would leak a sensitive value, collide with a
// standard attribute or not fit its field, as lint reports.
func contracts(table policy.Table) (map[string]contract, error) {
	out := make(map[string]contract, len(table))
	var errs []error
	for procedure, m := range table {
		c, err := contractOf(m)
		if err != nil {
			errs = append(errs, fmt.Errorf("telemetry: %s: %w", procedure, err))
			continue
		}
		out[procedure] = c
	}
	return out, errors.Join(errs...)
}

func contractOf(m policy.Method) (contract, error) {
	classified, err := classifications(m.Rules)
	if err != nil {
		return contract{}, err
	}
	c := contract{classified: classified}
	if m.Descriptor == nil {
		return c, nil
	}
	fields, err := requestAttributes(m.Descriptor.Input(), nil, map[protoreflect.FullName]bool{})
	if err != nil {
		return contract{}, err
	}
	keys := map[attribute.Key]bool{}
	for _, f := range fields {
		if keys[f.key] {
			return contract{}, fmt.Errorf("two request fields record the attribute %q", f.key)
		}
		keys[f.key] = true
	}
	c.fields = fields
	return c, nil
}

// classifications reads the error classifications, each of a reason the
// RPC declares.
func classifications(rules *dedov1.MethodRules) (map[string]bool, error) {
	if len(rules.GetErrorClassifications()) == 0 {
		return nil, nil
	}
	out := map[string]bool{}
	for _, c := range rules.GetErrorClassifications() {
		declared := slices.ContainsFunc(rules.GetErrors(), func(e *dedov1.ErrorReason) bool { return e.GetReason() == c.GetReason() })
		if !declared {
			return nil, fmt.Errorf("error classification of %q, a reason the RPC does not declare", c.GetReason())
		}
		out[c.GetReason()] = c.GetServerError()
	}
	return out, nil
}

// requestAttributes collects the declared attributes of a message's fields
// and of the fields of its singular message fields, depth first.
func requestAttributes(md protoreflect.MessageDescriptor, path []protoreflect.FieldDescriptor, seen map[protoreflect.FullName]bool) ([]fieldAttribute, error) {
	if seen[md.FullName()] || len(path) >= maxDepth {
		return nil, nil
	}
	seen[md.FullName()] = true
	defer delete(seen, md.FullName())
	var out []fieldAttribute
	fields := md.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		at := append(slices.Clone(path), fd)
		if rules, _ := proto.GetExtension(fd.Options(), dedov1.E_Field).(*dedov1.FieldRules); rules.GetAttribute() != "" || rules.GetAttributeMode() != dedov1.AttributeMode_ATTRIBUTE_MODE_UNSPECIFIED {
			if err := checkField(fd, rules); err != nil {
				return nil, err
			}
			out = append(out, fieldAttribute{key: attribute.Key(rules.GetAttribute()), mode: fieldattr.Mode(rules), path: at})
		}
		if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() {
			nested, err := requestAttributes(fd.Message(), at, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
		}
	}
	return out, nil
}

func checkField(fd protoreflect.FieldDescriptor, rules *dedov1.FieldRules) error {
	for _, problem := range []string{fieldattr.KeyProblem(rules.GetAttribute()), fieldattr.SensitiveProblem(rules), fieldattr.ModeProblem(fd, rules)} {
		if problem != "" {
			return fmt.Errorf("field %s: %s", fd.FullName(), problem)
		}
	}
	return nil
}

// attributes reads the declared attributes of a request. An attribute
// inside an unset message is left out.
func (c contract) attributes(req protoreflect.Message) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(c.fields))
	for _, f := range c.fields {
		if kv, ok := f.read(req); ok {
			out = append(out, kv)
		}
	}
	return out
}

func (f fieldAttribute) read(req protoreflect.Message) (attribute.KeyValue, bool) {
	msg := req
	last := len(f.path) - 1
	for _, fd := range f.path[:last] {
		if !msg.Has(fd) {
			return attribute.KeyValue{}, false
		}
		msg = msg.Get(fd).Message()
	}
	fd := f.path[last]
	switch f.mode {
	case dedov1.AttributeMode_ATTRIBUTE_MODE_PRESENCE:
		return f.key.Bool(msg.Has(fd)), true
	case dedov1.AttributeMode_ATTRIBUTE_MODE_ITEM_COUNT:
		if fd.IsMap() {
			return f.key.Int(msg.Get(fd).Map().Len()), true
		}
		return f.key.Int(msg.Get(fd).List().Len()), true
	default:
		return scalarValue(f.key, fd, msg.Get(fd))
	}
}

// scalarValue is a scalar or enum field's value as an attribute: an enum
// by its value name.
func scalarValue(key attribute.Key, fd protoreflect.FieldDescriptor, v protoreflect.Value) (attribute.KeyValue, bool) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return key.Bool(v.Bool()), true
	case protoreflect.StringKind:
		return key.String(v.String()), true
	case protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
			return key.String(string(ev.Name())), true
		}
		return key.Int64(int64(v.Enum())), true
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return key.Int64(v.Int()), true
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if v.Uint() > math.MaxInt64 {
			return key.String(fmt.Sprint(v.Uint())), true
		}
		return key.Int64(int64(v.Uint())), true //nolint:gosec // checked above
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return key.Float64(v.Float()), true
	default:
		return attribute.KeyValue{}, false
	}
}

// requestAttrs holds the attributes of the request being served, for the
// log records written while serving it. It is set once, on the first
// request message.
type requestAttrs struct {
	p atomic.Pointer[[]attribute.KeyValue]
}

type requestAttrsKey struct{}

// withRequestAttrs gives ctx a holder for the request's attributes; with
// nil, it hides any holder of a call that ctx was derived from.
func withRequestAttrs(ctx context.Context, holder *requestAttrs) context.Context {
	if holder == nil && ctx.Value(requestAttrsKey{}) == nil {
		return ctx
	}
	return context.WithValue(ctx, requestAttrsKey{}, holder)
}

// requestAttrsFrom returns the attributes of the request being served.
func requestAttrsFrom(ctx context.Context) []attribute.KeyValue {
	holder, _ := ctx.Value(requestAttrsKey{}).(*requestAttrs)
	if holder == nil {
		return nil
	}
	if p := holder.p.Load(); p != nil {
		return *p
	}
	return nil
}

// recordingStream records the declared attributes of the first request
// message it receives.
type recordingStream struct {
	connect.ServerStream
	once   sync.Once
	record func(proto.Message)
}

func (s *recordingStream) Receive(msg any) error {
	err := s.ServerStream.Receive(msg)
	if err == nil {
		if m, ok := msg.(proto.Message); ok {
			s.once.Do(func() { s.record(m) })
		}
	}
	return err
}
