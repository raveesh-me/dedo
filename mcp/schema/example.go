package schema

import (
	"encoding/base64"
	"math"
	"strconv"

	protovalidate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/raveesh-me/dedo/googletype"
)

// Examples come from protovalidate's example rules, which validation
// ignores, as the REST reference's do: the first example of a scalar, an
// enum, a timestamp, a duration or a wrapper, or of a repeated field's
// items, becomes the JSON Schema keyword examples. A field of a google.type
// message, whose proto declares none, gets the one package googletype
// knows. Unlike the reference, a tool schema derives none: an enum already
// lists its values, and a field mask or message would need proto field
// names that the examples, written for the JSON mapping, do not have.

// withExamples adds a field's example to its schema, if it declares one.
func withExamples(s Object, f *protogen.Field) Object {
	ex := fieldExample(f)
	if ex == nil {
		return s
	}
	return merge(s, Object{"examples": []any{ex}})
}

func fieldExample(f *protogen.Field) any {
	fd, rules := f.Desc, fieldRules(f)
	switch {
	case fd.IsMap():
		return nil
	case fd.IsList():
		if item := valueExample(fd, rules.GetRepeated().GetItems()); item != nil {
			return []any{item}
		}
		return knownExample(fd)
	}
	if ex := valueExample(fd, rules); ex != nil {
		return ex
	}
	return knownExample(fd)
}

// knownExample is the example dedo knows for a field of a googleapis
// message, such as google.type.PostalAddress.postal_code; nil for any
// other.
func knownExample(fd protoreflect.FieldDescriptor) any {
	doc, _ := googletype.Lookup(fd.FullName())
	return doc.Example
}

// valueExample returns the example of one value of a field, as proto3 JSON
// writes it, or nil.
func valueExample(fd protoreflect.FieldDescriptor, rules *protovalidate.FieldRules) any {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		if ex := rules.GetEnum().GetExample(); len(ex) > 0 {
			if v := fd.Enum().Values().ByNumber(protoreflect.EnumNumber(ex[0])); v != nil {
				return string(v.Name())
			}
		}
		return nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return wellKnownExample(fd.Message().FullName(), rules)
	}
	return scalarExample(fd.Kind(), rules)
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

func wellKnownExample(name protoreflect.FullName, rules *protovalidate.FieldRules) any {
	if kind, ok := wrapperKinds[name]; ok {
		return scalarExample(kind, rules)
	}
	switch name {
	case "google.protobuf.Timestamp":
		if t := rules.GetTimestamp().GetExample(); len(t) > 0 {
			return timestampJSON(t[0])
		}
	case "google.protobuf.Duration":
		if d := rules.GetDuration().GetExample(); len(d) > 0 {
			return durationJSON(d[0])
		}
	}
	return nil
}

func timestampJSON(t *timestamppb.Timestamp) any {
	if t.CheckValid() != nil {
		return nil
	}
	return t.AsTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
}

func durationJSON(d *durationpb.Duration) any {
	if d.CheckValid() != nil {
		return nil
	}
	return strconv.FormatFloat(d.AsDuration().Seconds(), 'f', -1, 64) + "s"
}

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

func first[T any](examples []T) any {
	if len(examples) == 0 {
		return nil
	}
	return examples[0]
}

// decimal writes a 64-bit example as proto3 JSON does: a string.
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

func finite(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}
