package rules

import (
	"context"
	"strings"

	protovalidate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/bufplugin/check"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

var fieldExampleRequired = lintRule(
	idFieldExample,
	"Checks that every scalar field a documented REST operation's request or response reaches declares a protovalidate example of its type, and that a resource name's example has its resource's form.",
	check.RuleHandlerFunc(checkFieldExamples),
)

// exampleRules names the protovalidate rules that hold the example of a
// well-known type: its own, or, for a wrapper, those of the type it wraps.
var exampleRules = map[protoreflect.FullName]protoreflect.Name{
	"google.protobuf.Timestamp":   "timestamp",
	"google.protobuf.Duration":    "duration",
	"google.protobuf.FieldMask":   "field_mask",
	"google.protobuf.StringValue": "string",
	"google.protobuf.BytesValue":  "bytes",
	"google.protobuf.BoolValue":   "bool",
	"google.protobuf.Int32Value":  "int32",
	"google.protobuf.UInt32Value": "uint32",
	"google.protobuf.Int64Value":  "int64",
	"google.protobuf.UInt64Value": "uint64",
	"google.protobuf.FloatValue":  "float",
	"google.protobuf.DoubleValue": "double",
}

// checkFieldExamples checks the fields of the module's messages that the
// REST reference documents: those reached, through fields, from the
// request or response of a non-deprecated RPC with an HTTP binding, of a
// service the generators document by default (any audience but
// AUDIENCE_COMPONENT_INTERNAL). Each field is checked once, however many
// RPCs reach it.
func checkFieldExamples(_ context.Context, w check.ResponseWriter, req check.Request) error {
	resources := resourcePatternIndex(req)
	for _, m := range documentedMessages(req) {
		fields := m.Fields()
		for i := range fields.Len() {
			checkFieldExample(w, fields.Get(i), resources)
		}
	}
	return nil
}

// documentedMessages returns the messages of the module's own files that
// documented operations reach, each once, in the order reached. Map
// entries are left out: their key and value are checked on the map field.
func documentedMessages(req check.Request) []protoreflect.MessageDescriptor {
	own := map[string]bool{}
	for _, fd := range req.FileDescriptors() {
		if !fd.IsImport() {
			own[fd.ProtoreflectFileDescriptor().Path()] = true
		}
	}
	seen := map[protoreflect.FullName]bool{}
	var out []protoreflect.MessageDescriptor
	var visit func(md protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		if seen[md.FullName()] {
			return
		}
		seen[md.FullName()] = true
		if own[md.ParentFile().Path()] && !md.IsMapEntry() {
			out = append(out, md)
		}
		fields := md.Fields()
		for i := range fields.Len() {
			if sub := fields.Get(i).Message(); sub != nil {
				visit(sub)
			}
		}
	}
	for _, fd := range req.FileDescriptors() {
		if fd.IsImport() {
			continue
		}
		services := fd.ProtoreflectFileDescriptor().Services()
		for i := range services.Len() {
			s := services.Get(i)
			if serviceRules(s).GetAudience() == dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL {
				continue
			}
			methods := s.Methods()
			for j := range methods.Len() {
				m := methods.Get(j)
				if isDeprecated(m) || !hasHTTPBinding(m) {
					continue
				}
				visit(m.Input())
				visit(m.Output())
			}
		}
	}
	return out
}

func hasHTTPBinding(m protoreflect.MethodDescriptor) bool {
	rule := httpRule(m)
	return rule != nil && rule.GetPattern() != nil
}

// checkFieldExample reports a field of a documented message that needs an
// example and has none, an enum example that is not one of its values,
// and a resource name example that has none of its resource's forms. A
// deprecated field needs none: the reference marks it deprecated, and the
// quality gate skips it.
func checkFieldExample(w check.ResponseWriter, f protoreflect.FieldDescriptor, resources map[string][]string) {
	if opts, _ := f.Options().(*descriptorpb.FieldOptions); opts.GetDeprecated() {
		return
	}
	rules := extension[*protovalidate.FieldRules](f.Options(), protovalidate.E_Field)
	switch {
	case f.IsMap():
		mapRules := rules.GetMap()
		checkValueExample(w, f, f.MapKey(), mapRules.GetKeys(), "map.keys.")
		checkValueExample(w, f, f.MapValue(), mapRules.GetValues(), "map.values.")
	case f.IsList():
		checkValueExample(w, f, f, rules.GetRepeated().GetItems(), "repeated.items.")
	default:
		checkValueExample(w, f, f, rules, "")
	}
	if f.Kind() == protoreflect.StringKind && !f.IsList() && !f.IsMap() {
		checkResourceExample(w, f, rules, resources)
	}
}

// checkValueExample checks the example of one value of field f: f itself,
// its items or its map's key or value, described by value and rules. prefix
// is where those rules sit in (buf.validate.field).
func checkValueExample(w check.ResponseWriter, f, value protoreflect.FieldDescriptor, rules *protovalidate.FieldRules, prefix string) {
	switch value.Kind() {
	case protoreflect.EnumKind:
		// The reference derives an enum's example: its first value after
		// the zero value. One that is declared must be one of its values.
		for _, n := range ruleExamples(rules, "enum") {
			if value.Enum().Values().ByNumber(protoreflect.EnumNumber(n.Int())) == nil {
				w.AddAnnotation(check.WithDescriptor(f),
					messagef(idFieldExample, "Field %q has the example %d, which is not a value of %s.", f.Name(), n.Int(), value.Enum().FullName()))
			}
		}
		return
	case protoreflect.MessageKind, protoreflect.GroupKind:
		// A message's schema documents its fields, and the reference
		// composes a list's or map's example from theirs. Only well-known
		// types written as one JSON value need an example of their own.
		name, ok := exampleRules[value.Message().FullName()]
		if !ok {
			return
		}
		requireExample(w, f, rules, prefix, name)
		return
	}
	requireExample(w, f, rules, prefix, protoreflect.Name(value.Kind().String()))
}

func requireExample(w check.ResponseWriter, f protoreflect.FieldDescriptor, rules *protovalidate.FieldRules, prefix string, name protoreflect.Name) {
	if len(ruleExamples(rules, name)) > 0 {
		return
	}
	w.AddAnnotation(check.WithDescriptor(f),
		messagef(idFieldExample, "Field %q needs an example: set (buf.validate.field).%s%s.example to a realistic value.", f.Name(), prefix, name))
}

// ruleExamples returns the examples in the rules of one type, such as
// "string" or "timestamp", of a field's rules.
func ruleExamples(rules *protovalidate.FieldRules, name protoreflect.Name) []protoreflect.Value {
	if rules == nil {
		return nil
	}
	r := rules.ProtoReflect()
	typed := r.Descriptor().Fields().ByName(name)
	if typed == nil || !r.Has(typed) {
		return nil
	}
	sub := r.Get(typed).Message()
	ex := sub.Descriptor().Fields().ByName("example")
	if ex == nil {
		return nil
	}
	list := sub.Get(ex).List()
	out := make([]protoreflect.Value, 0, list.Len())
	for i := range list.Len() {
		out = append(out, list.Get(i))
	}
	return out
}

// checkResourceExample reports a resource name example that has none of
// its resource's forms: the reference takes its path parameters' examples
// from the segments of the bound field's example.
func checkResourceExample(w check.ResponseWriter, f protoreflect.FieldDescriptor, rules *protovalidate.FieldRules, resources map[string][]string) {
	patterns := namePatterns(f, resources)
	if len(patterns) == 0 {
		return
	}
	for _, ex := range ruleExamples(rules, "string") {
		if !matchesAny(ex.String(), patterns) {
			w.AddAnnotation(check.WithDescriptor(f),
				messagef(idFieldExample, "Field %q has the example %q, which is not a name of the form %s.", f.Name(), ex.String(), strings.Join(patterns, " or ")))
		}
	}
}

// namePatterns returns the patterns of the resource a field names: its
// message's, for the name field of a resource, or else its
// resource_reference's type's.
func namePatterns(f protoreflect.FieldDescriptor, resources map[string][]string) []string {
	if ref := extension[*annotations.ResourceReference](f.Options(), annotations.E_ResourceReference); ref.GetType() != "" {
		return resources[ref.GetType()]
	}
	if f.Name() != "name" {
		return nil
	}
	if res := extension[*annotations.ResourceDescriptor](f.ContainingMessage().Options(), annotations.E_Resource); res != nil {
		return res.GetPattern()
	}
	return nil
}

// resourcePatternIndex maps every resource type of the request's files,
// imports included, to its patterns.
func resourcePatternIndex(req check.Request) map[string][]string {
	index := map[string][]string{}
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := range msgs.Len() {
			m := msgs.Get(i)
			if res := extension[*annotations.ResourceDescriptor](m.Options(), annotations.E_Resource); res != nil {
				index[res.GetType()] = res.GetPattern()
			}
			walk(m.Messages())
		}
	}
	for _, fd := range req.FileDescriptors() {
		f := fd.ProtoreflectFileDescriptor()
		defs, _ := proto.GetExtension(f.Options(), annotations.E_ResourceDefinition).([]*annotations.ResourceDescriptor)
		for _, d := range defs {
			index[d.GetType()] = d.GetPattern()
		}
		walk(f.Messages())
	}
	return index
}

// matchesAny reports whether name has the form of one of the patterns:
// each literal segment the same, each variable one non-empty segment.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesPattern(name, p) {
			return true
		}
	}
	return false
}

func matchesPattern(name, pattern string) bool {
	segments := strings.Split(name, "/")
	parts := strings.Split(pattern, "/")
	if len(segments) != len(parts) {
		return false
	}
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "{"):
			if segments[i] == "" {
				return false
			}
		case p != segments[i]:
			return false
		}
	}
	return true
}
