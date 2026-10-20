// Package schema builds the JSON Schema of an RPC's MCP tool input from its
// request message at code-generation time, when comments are still
// available. It follows the proto3 JSON mapping with proto field names (the
// names MCP tool calls use here) and google.api.field_behavior:
//
//   - OUTPUT_ONLY fields are left out: the server sets them.
//   - A request with a google.protobuf.FieldMask is a partial update (AIP-134):
//     inside the resource only the IDENTIFIER (its name) is required.
//   - Any other request carrying a resource creates it (AIP-133): the
//     resource's IDENTIFIER is left out, since the server assigns the name.
//   - REQUIRED fields are required everywhere else.
//
// Every field carries its leading comment as its description, and a field
// mask lists the paths it accepts.
package schema

import (
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// maxDepth bounds recursive messages.
const maxDepth = 4

// Object is a JSON Schema object. encoding/json writes its keys sorted, so
// the output is deterministic.
type Object = map[string]any

type mode int

const (
	modePlain  mode = iota // REQUIRED fields are required
	modeCreate             // the resource of a create: no IDENTIFIER
	modePatch              // the resource of a partial update: only IDENTIFIER is required
	modeOutput             // a response: every field, none required
)

// Output returns the output schema of a unary RPC's MCP tool: the response
// message as the tool's structuredContent carries it (proto field names,
// output-only fields included, nothing required).
func Output(m *protogen.Method) Object {
	return message(m.Output, modeOutput, 0)
}

// Input returns the input schema of a unary RPC's MCP tool.
func Input(m *protogen.Method) Object {
	out := message(m.Input, modePlain, 0)
	maskPaths := patchPaths(m.Input)
	props := out["properties"].(Object)
	for _, f := range m.Input.Fields {
		if !isResource(f) {
			continue
		}
		mode := modeCreate
		if maskPaths != nil {
			mode = modePatch
		}
		props[string(f.Desc.Name())] = withDescription(message(f.Message, mode, 1), comment(f))
	}
	for _, f := range m.Input.Fields {
		if isFieldMask(f) && maskPaths != nil {
			props[string(f.Desc.Name())] = Object{
				"type": "string",
				"description": join(comment(f), "Comma-separated paths of the fields to change, in lowerCamelCase. Valid paths: "+
					strings.Join(maskPaths, ", ")+"."),
			}
		}
	}
	return out
}

// message builds the schema of a message in the given mode.
func message(msg *protogen.Message, mode mode, depth int) Object {
	if wkt := wellKnown(msg.Desc); wkt != nil {
		return wkt
	}
	out := Object{"type": "object", "properties": Object{}}
	if d := messageDescription(msg); d != "" {
		out["description"] = d
	}
	if depth > maxDepth {
		return out
	}
	var required []string
	for _, f := range msg.Fields {
		if mode != modeOutput && has(f, annotations.FieldBehavior_OUTPUT_ONLY) {
			continue
		}
		identifier := has(f, annotations.FieldBehavior_IDENTIFIER)
		if identifier && mode == modeCreate {
			continue
		}
		name := string(f.Desc.Name())
		out["properties"].(Object)[name] = withDescription(field(f, depth, mode), comment(f))
		switch {
		case mode == modeOutput:
		case mode == modePatch && identifier:
			required = append(required, name)
		case mode != modePatch && has(f, annotations.FieldBehavior_REQUIRED):
			required = append(required, name)
		}
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func field(f *protogen.Field, depth int, mode mode) Object {
	switch {
	case f.Desc.IsMap():
		return Object{"type": "object", "additionalProperties": single(f.Message.Fields[1], depth, mode)}
	case f.Desc.IsList():
		return Object{"type": "array", "items": single(f, depth, mode)}
	}
	return single(f, depth, mode)
}

// single builds a field's schema. Nested messages of an output stay output;
// any other nested message is plain.
func single(f *protogen.Field, depth int, mode mode) Object {
	switch f.Desc.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := modePlain
		if mode == modeOutput {
			nested = modeOutput
		}
		return message(f.Message, nested, depth+1)
	case protoreflect.EnumKind:
		var names []string
		for _, v := range f.Enum.Values {
			names = append(names, string(v.Desc.Name()))
		}
		return Object{"type": "string", "enum": names}
	}
	return scalar(f.Desc.Kind())
}

func scalar(kind protoreflect.Kind) Object {
	switch kind {
	case protoreflect.BoolKind:
		return Object{"type": "boolean"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return Object{"type": "integer"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		// The JSON mapping writes 64-bit integers as strings and reads both.
		return Object{"type": []string{"string", "integer"}}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return Object{"type": "number"}
	case protoreflect.BytesKind:
		return Object{"type": "string", "contentEncoding": "base64"}
	}
	return Object{"type": "string"}
}

// wellKnown returns the JSON-mapping schema of a well-known type, or nil.
func wellKnown(md protoreflect.MessageDescriptor) Object {
	switch md.FullName() {
	case "google.protobuf.Timestamp":
		return Object{"type": "string", "format": "date-time"}
	case "google.protobuf.Duration":
		return Object{"type": "string", "description": `A duration in seconds with an "s" suffix, such as "90s".`}
	case "google.protobuf.FieldMask":
		return Object{"type": "string", "description": "Comma-separated field paths in lowerCamelCase."}
	case "google.protobuf.Empty":
		return Object{"type": "object"}
	case "google.protobuf.Struct":
		return Object{"type": "object"}
	case "google.protobuf.Value":
		return Object{}
	case "google.protobuf.ListValue":
		return Object{"type": "array"}
	case "google.protobuf.Any":
		return Object{"type": "object", "properties": Object{"@type": Object{"type": "string"}}}
	case "google.protobuf.StringValue", "google.protobuf.BytesValue":
		return Object{"type": "string"}
	case "google.protobuf.BoolValue":
		return Object{"type": "boolean"}
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return Object{"type": "integer"}
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return Object{"type": []string{"string", "integer"}}
	case "google.protobuf.FloatValue", "google.protobuf.DoubleValue":
		return Object{"type": "number"}
	}
	return nil
}

// patchPaths returns the field mask paths a partial-update request accepts:
// the mutable fields of its resource, in lowerCamelCase. It returns nil for
// a request without a field mask or a resource.
func patchPaths(req *protogen.Message) []string {
	var hasMask bool
	var resource *protogen.Field
	for _, f := range req.Fields {
		hasMask = hasMask || isFieldMask(f)
		if isResource(f) {
			resource = f
		}
	}
	if !hasMask || resource == nil {
		return nil
	}
	paths := []string{}
	for _, f := range resource.Message.Fields {
		if has(f, annotations.FieldBehavior_OUTPUT_ONLY) || has(f, annotations.FieldBehavior_IDENTIFIER) ||
			has(f, annotations.FieldBehavior_IMMUTABLE) {
			continue
		}
		paths = append(paths, f.Desc.JSONName())
	}
	return paths
}

func isResource(f *protogen.Field) bool {
	if f.Message == nil {
		return false
	}
	res, _ := proto.GetExtension(f.Message.Desc.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
	return res != nil
}

func isFieldMask(f *protogen.Field) bool {
	return f.Message != nil && f.Message.Desc.FullName() == "google.protobuf.FieldMask"
}

func has(f *protogen.Field, want annotations.FieldBehavior) bool {
	behaviors, _ := proto.GetExtension(f.Desc.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	for _, b := range behaviors {
		if b == want {
			return true
		}
	}
	return false
}

// messageDescription prefers the consumer-facing message option, then the
// leading comment.
func messageDescription(msg *protogen.Message) string {
	rules, _ := proto.GetExtension(msg.Desc.Options(), dedov1.E_Message).(*dedov1.MessageRules)
	if d := strings.TrimSpace(rules.GetDescription()); d != "" {
		return d
	}
	return clean(msg.Comments.Leading)
}

func comment(f *protogen.Field) string { return clean(f.Comments.Leading) }

func clean(c protogen.Comments) string {
	lines := strings.Split(strings.TrimSpace(string(c)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

// withDescription sets a field's description, keeping the type's own one
// after it, so "The to-do to update." comes before what a to-do is.
func withDescription(s Object, d string) Object {
	if d == "" {
		return s
	}
	out := Object{}
	for k, v := range s {
		out[k] = v
	}
	own, _ := s["description"].(string)
	out["description"] = join(d, own)
	return out
}

func join(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
