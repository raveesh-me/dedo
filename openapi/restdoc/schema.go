package restdoc

import (
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// schemas builds component schemas on demand, following the proto3 JSON
// mapping, which is what REST callers send and receive.
type schemas struct {
	messages map[protoreflect.FullName]*protogen.Message
	names    map[protoreflect.FullName]string
	built    map[string]*Schema
}

func newSchemas(files []*protogen.File) *schemas {
	s := &schemas{
		messages: map[protoreflect.FullName]*protogen.Message{},
		names:    map[protoreflect.FullName]string{},
		built:    map[string]*Schema{},
	}
	var all []*protogen.Message
	for _, f := range files {
		all = append(all, collectMessages(f.Messages)...)
	}
	short := map[string]int{}
	for _, m := range all {
		s.messages[m.Desc.FullName()] = m
		short[string(m.Desc.Name())]++
	}
	// Short names read better in docs; full names keep them unique.
	for _, m := range all {
		name := string(m.Desc.Name())
		if short[name] > 1 {
			name = string(m.Desc.FullName())
		}
		s.names[m.Desc.FullName()] = name
	}
	return s
}

func collectMessages(msgs []*protogen.Message) []*protogen.Message {
	var out []*protogen.Message
	for _, m := range msgs {
		if m.Desc.IsMapEntry() {
			continue
		}
		out = append(out, m)
		out = append(out, collectMessages(m.Messages)...)
	}
	return out
}

// ref returns a reference to the message's component schema, building it
// the first time.
func (s *schemas) ref(md protoreflect.MessageDescriptor) *Schema {
	if wkt := wellKnown(md); wkt != nil {
		return wkt
	}
	name := s.names[md.FullName()]
	if _, ok := s.built[name]; !ok {
		s.built[name] = &Schema{} // placeholder breaks recursion
		s.built[name] = s.object(s.messages[md.FullName()], nil)
	}
	return &Schema{Ref: "#/components/schemas/" + name}
}

// object builds the schema of a message, leaving out the skipped fields.
func (s *schemas) object(m *protogen.Message, skip map[string]bool) *Schema {
	out := &Schema{
		Type:        "object",
		Description: messageDescription(m),
		Properties:  map[string]*Schema{},
	}
	for _, f := range m.Fields {
		if skip[string(f.Desc.Name())] {
			continue
		}
		fs := s.field(f.Desc)
		fs.Description = cleanComment(f.Comments.Leading)
		applyFieldOptions(fs, f.Desc)
		out.Properties[f.Desc.JSONName()] = fs
		if hasBehavior(f.Desc, annotations.FieldBehavior_REQUIRED) {
			out.Required = append(out.Required, f.Desc.JSONName())
		}
	}
	return out
}

// field returns the schema of a field's value.
func (s *schemas) field(fd protoreflect.FieldDescriptor) *Schema {
	switch {
	case fd.IsMap():
		return &Schema{Type: "object", AdditionalProperties: s.single(fd.MapValue())}
	case fd.IsList():
		return &Schema{Type: "array", Items: s.single(fd)}
	}
	return s.single(fd)
}

func (s *schemas) single(fd protoreflect.FieldDescriptor) *Schema {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return s.ref(fd.Message())
	case protoreflect.EnumKind:
		var values []string
		for i := 0; i < fd.Enum().Values().Len(); i++ {
			values = append(values, string(fd.Enum().Values().Get(i).Name()))
		}
		return &Schema{Type: "string", Enum: values}
	}
	return scalar(fd.Kind())
}

func scalar(kind protoreflect.Kind) *Schema {
	switch kind {
	case protoreflect.BoolKind:
		return &Schema{Type: "boolean"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return &Schema{Type: "integer", Format: "int32"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return &Schema{Type: "integer", Format: "uint32"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		// The JSON mapping writes 64-bit integers as strings.
		return &Schema{Type: "string", Format: "int64"}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return &Schema{Type: "string", Format: "uint64"}
	case protoreflect.FloatKind:
		return &Schema{Type: "number", Format: "float"}
	case protoreflect.DoubleKind:
		return &Schema{Type: "number", Format: "double"}
	case protoreflect.BytesKind:
		return &Schema{Type: "string", Format: "byte"}
	}
	return &Schema{Type: "string"}
}

// wellKnown returns the JSON-mapping schema of a well-known type, or nil.
func wellKnown(md protoreflect.MessageDescriptor) *Schema {
	switch md.FullName() {
	case "google.protobuf.Timestamp":
		return &Schema{Type: "string", Format: "date-time"}
	case "google.protobuf.Duration":
		return &Schema{Type: "string", Pattern: `^-?\d+(\.\d+)?s$`, Description: "A duration in seconds, such as \"30s\"."}
	case "google.protobuf.FieldMask":
		return &Schema{Type: "string", Description: "Comma-separated lowerCamelCase field paths."}
	case "google.protobuf.Empty":
		return &Schema{Type: "object"}
	case "google.protobuf.Struct":
		return &Schema{Type: "object", AdditionalProperties: &Schema{}}
	case "google.protobuf.Value":
		return &Schema{}
	case "google.protobuf.ListValue":
		return &Schema{Type: "array", Items: &Schema{}}
	case "google.protobuf.Any":
		return &Schema{Type: "object", Properties: map[string]*Schema{"@type": {Type: "string"}}, AdditionalProperties: &Schema{}}
	case "google.protobuf.StringValue", "google.protobuf.BytesValue",
		"google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return &Schema{Type: []string{"string", "null"}}
	case "google.protobuf.BoolValue":
		return &Schema{Type: []string{"boolean", "null"}}
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return &Schema{Type: []string{"integer", "null"}}
	case "google.protobuf.FloatValue", "google.protobuf.DoubleValue":
		return &Schema{Type: []string{"number", "null"}}
	}
	return nil
}

// isQueryable reports whether a field can be a query parameter: a scalar,
// an enum, a repeated scalar, or a well-known type written as a string.
func isQueryable(fd protoreflect.FieldDescriptor) bool {
	if fd.IsMap() {
		return false
	}
	if fd.Kind() != protoreflect.MessageKind {
		return true
	}
	wkt := wellKnown(fd.Message())
	return wkt != nil && wkt.Type != "object" && wkt.Type != "array" && wkt.Type != nil
}

func applyFieldOptions(s *Schema, fd protoreflect.FieldDescriptor) {
	opts, _ := fd.Options().(*descriptorpb.FieldOptions)
	s.Deprecated = opts.GetDeprecated()
	s.ReadOnly = hasBehavior(fd, annotations.FieldBehavior_OUTPUT_ONLY)
	s.WriteOnly = hasBehavior(fd, annotations.FieldBehavior_INPUT_ONLY)
	rules, _ := proto.GetExtension(fd.Options(), dedov1.E_Field).(*dedov1.FieldRules)
	s.Sensitive = rules.GetSensitive()
	if s.Ref != "" {
		// A $ref sibling only keeps its description in OpenAPI 3.1; keep
		// the reference clean otherwise.
		s.Deprecated, s.ReadOnly, s.WriteOnly = false, false, false
	}
}

func hasBehavior(fd protoreflect.FieldDescriptor, want annotations.FieldBehavior) bool {
	behaviors, _ := proto.GetExtension(fd.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	for _, b := range behaviors {
		if b == want {
			return true
		}
	}
	return false
}

// messageDescription prefers the consumer-facing message option and falls
// back to the leading comment.
func messageDescription(m *protogen.Message) string {
	rules, _ := proto.GetExtension(m.Desc.Options(), dedov1.E_Message).(*dedov1.MessageRules)
	if d := strings.TrimSpace(rules.GetDescription()); d != "" {
		return d
	}
	return cleanComment(m.Comments.Leading)
}

// cleanComment trims the comment markers' leading space from every line.
func cleanComment(c protogen.Comments) string {
	lines := strings.Split(strings.TrimSpace(string(c)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
