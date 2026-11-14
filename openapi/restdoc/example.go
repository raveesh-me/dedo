package restdoc

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	protovalidate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maxExampleDepth bounds how deep an example of a list or map of messages
// nests messages.
const maxExampleDepth = 3

// Examples come from protovalidate's example rules, which validation
// ignores: string name = 1 [(buf.validate.field).string.example = "..."].
// Each is written in the proto3 JSON mapping the REST API speaks, so it
// matches the schema's declared type. Only the first example of a field is
// used.

// fieldExample returns the example of a field's value, or nil: the field's
// own for a singular field; a list of its item's for a repeated one; an
// object of its key's and value's for a map. An enum without one gets its
// first value after the zero value. A singular message has no example of
// its own: its schema documents its fields.
func fieldExample(fd protoreflect.FieldDescriptor) any {
	return fieldExampleAt(fd, nil)
}

// fieldExampleAt is fieldExample for a field of a message composed inside
// the messages of outer.
func fieldExampleAt(fd protoreflect.FieldDescriptor, outer []protoreflect.FullName) any {
	rules := fieldRules(fd)
	switch {
	case fd.IsMap():
		key := valueExample(fd.MapKey(), rules.GetMap().GetKeys(), outer)
		value := valueExample(fd.MapValue(), rules.GetMap().GetValues(), outer)
		if key == nil || value == nil {
			return nil
		}
		return map[string]any{mapKey(key): value}
	case fd.IsList():
		item := valueExample(fd, rules.GetRepeated().GetItems(), outer)
		if item == nil {
			return nil
		}
		return []any{item}
	case fd.Message() != nil && wellKnown(fd.Message()) == nil:
		return nil
	}
	if ex := valueExample(fd, rules, outer); ex != nil {
		return ex
	}
	return knownExamples[fd.FullName()]
}

// knownExamples are examples of common googleapis fields, whose protos
// declare none and which a service cannot change.
var knownExamples = map[protoreflect.FullName]any{
	"google.type.Money.currency_code": "USD",
	"google.type.Money.units":         "42",
	"google.type.Money.nanos":         500000000,
}

// valueExample returns the example of one value of a field, from rules: a
// scalar's, an enum's, a well-known type's, or, for a message, an object of
// its fields' examples.
func valueExample(fd protoreflect.FieldDescriptor, rules *protovalidate.FieldRules, outer []protoreflect.FullName) any {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		return enumExample(fd.Enum(), rules.GetEnum().GetExample())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if ex, ok := wellKnownExample(fd.Message(), rules); ok {
			return ex
		}
		return messageExample(fd.Message(), outer)
	}
	return scalarExample(fd.Kind(), rules)
}

// scalarExample returns the first example of a scalar kind's rules, as
// proto3 JSON writes it: 64-bit integers as strings, bytes in base64.
func scalarExample(kind protoreflect.Kind, rules *protovalidate.FieldRules) any {
	switch kind {
	case protoreflect.StringKind:
		return first(rules.GetString_().GetExample())
	case protoreflect.BoolKind:
		return first(rules.GetBool().GetExample())
	case protoreflect.BytesKind:
		if b := rules.GetBytes().GetExample(); len(b) > 0 {
			return base64.StdEncoding.EncodeToString(b[0])
		}
	case protoreflect.Int32Kind:
		return first(rules.GetInt32().GetExample())
	case protoreflect.Sint32Kind:
		return first(rules.GetSint32().GetExample())
	case protoreflect.Sfixed32Kind:
		return first(rules.GetSfixed32().GetExample())
	case protoreflect.Uint32Kind:
		return first(rules.GetUint32().GetExample())
	case protoreflect.Fixed32Kind:
		return first(rules.GetFixed32().GetExample())
	case protoreflect.Int64Kind:
		return decimal(rules.GetInt64().GetExample())
	case protoreflect.Sint64Kind:
		return decimal(rules.GetSint64().GetExample())
	case protoreflect.Sfixed64Kind:
		return decimal(rules.GetSfixed64().GetExample())
	case protoreflect.Uint64Kind:
		return decimal(rules.GetUint64().GetExample())
	case protoreflect.Fixed64Kind:
		return decimal(rules.GetFixed64().GetExample())
	case protoreflect.FloatKind:
		if ex := rules.GetFloat().GetExample(); len(ex) > 0 {
			return finite(float64(ex[0]))
		}
	case protoreflect.DoubleKind:
		if ex := rules.GetDouble().GetExample(); len(ex) > 0 {
			return finite(ex[0])
		}
	}
	return nil
}

// wrapperKinds maps the wrapper types to the kind they wrap, whose rules
// protovalidate applies to them.
var wrapperKinds = map[protoreflect.FullName]protoreflect.Kind{
	"google.protobuf.StringValue": protoreflect.StringKind,
	"google.protobuf.BytesValue":  protoreflect.BytesKind,
	"google.protobuf.BoolValue":   protoreflect.BoolKind,
	"google.protobuf.Int32Value":  protoreflect.Int32Kind,
	"google.protobuf.UInt32Value": protoreflect.Uint32Kind,
	"google.protobuf.Int64Value":  protoreflect.Int64Kind,
	"google.protobuf.UInt64Value": protoreflect.Uint64Kind,
	"google.protobuf.FloatValue":  protoreflect.FloatKind,
	"google.protobuf.DoubleValue": protoreflect.DoubleKind,
}

// wellKnownExample returns the example of a well-known type that has rules
// of its own (Timestamp, Duration, FieldMask) or takes its value's (the
// wrappers). ok is false for any other message.
func wellKnownExample(md protoreflect.MessageDescriptor, rules *protovalidate.FieldRules) (any, bool) {
	if kind, ok := wrapperKinds[md.FullName()]; ok {
		return scalarExample(kind, rules), true
	}
	var ex proto.Message
	switch md.FullName() {
	case "google.protobuf.Timestamp":
		if t := rules.GetTimestamp().GetExample(); len(t) > 0 {
			ex = t[0]
		}
	case "google.protobuf.Duration":
		if d := rules.GetDuration().GetExample(); len(d) > 0 {
			ex = d[0]
		}
	case "google.protobuf.FieldMask":
		if m := rules.GetFieldMask().GetExample(); len(m) > 0 {
			ex = m[0]
		}
	default:
		return nil, false
	}
	if ex == nil {
		return nil, true
	}
	return protoJSON(ex), true
}

// protoJSON is a well-known type's value in its JSON mapping, or nil when
// it has none, such as a timestamp out of range.
func protoJSON(m proto.Message) any {
	b, err := protojson.Marshal(m)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

// enumExample is the name of the enum's example value, or else of its
// first value after the zero value, which proto3 reserves for
// "unspecified": a real value, and always one of the schema's enum.
func enumExample(ed protoreflect.EnumDescriptor, examples []int32) any {
	values := ed.Values()
	if len(examples) > 0 {
		if v := values.ByNumber(protoreflect.EnumNumber(examples[0])); v != nil {
			return string(v.Name())
		}
	}
	for i := range values.Len() {
		if v := values.Get(i); v.Number() != 0 {
			return string(v.Name())
		}
	}
	if values.Len() > 0 {
		return string(values.Get(0).Name())
	}
	return nil
}

// messageExample composes an object of the examples of a message's fields,
// for a list or map of messages, whose schema needs an example of its own.
// It is nil when no field has one. A message inside itself is left out, so
// a recursive message's example ends.
func messageExample(md protoreflect.MessageDescriptor, outer []protoreflect.FullName) any {
	if len(outer) >= maxExampleDepth || slices.Contains(outer, md.FullName()) || wellKnown(md) != nil {
		return nil
	}
	inner := append(slices.Clone(outer), md.FullName())
	out := map[string]any{}
	fields := md.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		ex := fieldExampleAt(fd, inner)
		if fd.Message() != nil && !fd.IsMap() && !fd.IsList() && wellKnown(fd.Message()) == nil {
			ex = messageExample(fd.Message(), inner)
		}
		if ex != nil {
			out[fd.JSONName()] = ex
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fieldRules(fd protoreflect.FieldDescriptor) *protovalidate.FieldRules {
	rules, _ := proto.GetExtension(fd.Options(), protovalidate.E_Field).(*protovalidate.FieldRules)
	return rules
}

// first returns the first example, or nil.
func first[T any](examples []T) any {
	if len(examples) == 0 {
		return nil
	}
	return examples[0]
}

// decimal writes the first 64-bit example as proto3 JSON does: a string.
func decimal[T int64 | uint64](examples []T) any {
	if len(examples) == 0 {
		return nil
	}
	switch v := any(examples[0]).(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return nil
}

// finite leaves out NaN and the infinities, which proto3 JSON writes as
// strings that a number schema does not admit.
func finite(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

// mapKey writes a map key's example as the JSON object key it becomes.
func mapKey(key any) string {
	if s, ok := key.(string); ok {
		return s
	}
	b, _ := json.Marshal(key)
	return string(b)
}

// segmentExample is the part of the bound field's example that a path
// parameter stands for: the matching segment of a resource name example,
// such as "123" for {book} in shelves/{shelf}/books/{book} from
// "shelves/1/books/123". It is nil when the example does not have the
// template's form, or the parameter stands for several segments.
func segmentExample(full, name string, example any) any {
	s, ok := example.(string)
	if !ok || s == "" {
		return nil
	}
	parts := strings.Split(full, "/")
	values := strings.Split(s, "/")
	if len(parts) != len(values) {
		return nil
	}
	var found any
	for i, p := range parts {
		switch {
		case p == "{"+name+"}":
			found = values[i]
		case strings.HasPrefix(p, "{"):
			if values[i] == "" {
				return nil
			}
		case p != values[i]:
			return nil
		}
	}
	return found
}

// withExample sets a query parameter's schema example from its field.
func withExample(s *Schema, fd protoreflect.FieldDescriptor) *Schema {
	s.Example = fieldExample(fd)
	return s
}

// pathExample is a path parameter's example, from its bound field's.
func (g *generator) pathExample(input *protogen.Message, p pathParam) any {
	fd := fieldAt(input.Desc, p.field)
	if fd == nil {
		return nil
	}
	return segmentExample(p.full, p.name, fieldExample(fd))
}

// fieldAt follows a field path such as "book.name" from a message.
func fieldAt(md protoreflect.MessageDescriptor, path string) protoreflect.FieldDescriptor {
	var fd protoreflect.FieldDescriptor
	for _, name := range strings.Split(path, ".") {
		if md == nil {
			return nil
		}
		fd = md.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			return nil
		}
		md = fd.Message()
	}
	return fd
}
