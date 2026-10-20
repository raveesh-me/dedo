package schema

import (
	"regexp"
	"strings"

	protovalidate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// knownConstraints are the documented limits of common googleapis fields,
// which their protos state only in comments.
var knownConstraints = map[protoreflect.FullName]Object{
	"google.type.Money.currency_code": {"pattern": "^[A-Z]{3}$"},
	"google.type.Money.nanos":         {"minimum": -999999999, "maximum": 999999999},
}

// bufValidateKeywords maps buf.validate rule names to JSON Schema keywords.
// Number rules have the same names for every numeric type.
var bufValidateKeywords = map[protoreflect.Name]string{
	"gte":       "minimum",
	"lte":       "maximum",
	"gt":        "exclusiveMinimum",
	"lt":        "exclusiveMaximum",
	"min_len":   "minLength",
	"max_len":   "maxLength",
	"pattern":   "pattern",
	"in":        "enum",
	"min_items": "minItems",
	"max_items": "maxItems",
}

// bufValidateFormats maps boolean buf.validate string rules to formats.
var bufValidateFormats = map[protoreflect.Name]string{
	"email": "email",
	"uri":   "uri",
	"uuid":  "uuid",
}

// constraints returns the JSON Schema keywords that a single value's
// declarations imply: its resource name pattern, known googleapis limits,
// and its buf.validate rules (for a repeated field, the rules of its items).
// The server enforces each of them, so stating them in the schema lets a
// model get a call right the first time.
func constraints(f *protogen.Field) Object {
	out := Object{}
	if patterns := resourcePatterns(f); len(patterns) > 0 && f.Desc.Kind() == protoreflect.StringKind {
		out["pattern"] = patternRegexp(patterns)
	}
	for k, v := range knownConstraints[f.Desc.FullName()] {
		out[k] = v
	}
	rules := fieldRules(f)
	if f.Desc.IsList() {
		rules = rules.GetRepeated().GetItems()
	}
	for k, v := range ruleKeywords(rules) {
		out[k] = v
	}
	return out
}

// listConstraints returns the buf.validate keywords of a repeated field as
// a whole: minItems and maxItems.
func listConstraints(f *protogen.Field) Object {
	return ruleKeywords(fieldRules(f).GetRepeated())
}

func fieldRules(f *protogen.Field) *protovalidate.FieldRules {
	rules, _ := proto.GetExtension(f.Desc.Options(), protovalidate.E_Field).(*protovalidate.FieldRules)
	return rules
}

// ruleKeywords maps the rules in a buf.validate rules message to JSON Schema
// keywords. Given FieldRules, it reads the rules of the field's type, such
// as int32 or string; given RepeatedRules, the item counts.
func ruleKeywords(rules proto.Message) Object {
	out := Object{}
	if rules == nil || !rules.ProtoReflect().IsValid() {
		return out
	}
	collect := func(rule protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if format, ok := bufValidateFormats[rule.Name()]; ok && v.Bool() {
			out["format"] = format
		}
		if keyword, ok := bufValidateKeywords[rule.Name()]; ok {
			out[keyword] = jsonValue(rule, v)
		}
		return true
	}
	if _, ok := rules.(*protovalidate.FieldRules); !ok {
		rules.ProtoReflect().Range(collect)
		return out
	}
	rules.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.ContainingOneof() != nil && fd.Name() != "repeated" && fd.Message() != nil {
			v.Message().Range(collect)
		}
		return true
	})
	return out
}

func jsonValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	if fd.IsList() {
		var items []any
		for i := range v.List().Len() {
			items = append(items, v.List().Get(i).Interface())
		}
		return items
	}
	return v.Interface()
}

// resourcePatterns returns the name patterns of the resource a field names:
// its own resource for an IDENTIFIER, or the referenced type.
func resourcePatterns(f *protogen.Field) []string {
	if has(f, annotations.FieldBehavior_IDENTIFIER) {
		res, _ := proto.GetExtension(f.Parent.Desc.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
		return res.GetPattern()
	}
	ref, _ := proto.GetExtension(f.Desc.Options(), annotations.E_ResourceReference).(*annotations.ResourceReference)
	if ref.GetType() == "" {
		return nil
	}
	return findResource(f.Desc.ParentFile(), ref.GetType(), map[string]bool{}).GetPattern()
}

// findResource looks for a resource type in a file and its imports.
func findResource(fd protoreflect.FileDescriptor, typ string, seen map[string]bool) *annotations.ResourceDescriptor {
	if seen[fd.Path()] {
		return nil
	}
	seen[fd.Path()] = true
	defs, _ := proto.GetExtension(fd.Options(), annotations.E_ResourceDefinition).([]*annotations.ResourceDescriptor)
	for _, d := range defs {
		if d.GetType() == typ {
			return d
		}
	}
	if res := findInMessages(fd.Messages(), typ); res != nil {
		return res
	}
	for i := range fd.Imports().Len() {
		if res := findResource(fd.Imports().Get(i).FileDescriptor, typ, seen); res != nil {
			return res
		}
	}
	return nil
}

func findInMessages(msgs protoreflect.MessageDescriptors, typ string) *annotations.ResourceDescriptor {
	for i := range msgs.Len() {
		md := msgs.Get(i)
		if res, _ := proto.GetExtension(md.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor); res.GetType() == typ {
			return res
		}
		if res := findInMessages(md.Messages(), typ); res != nil {
			return res
		}
	}
	return nil
}

var patternVariable = regexp.MustCompile(`\{[^}]+\}`)

// patternRegexp turns resource patterns such as "todos/{todo}" into one
// anchored regular expression: ^todos/[^/]+$.
func patternRegexp(patterns []string) string {
	alternatives := make([]string, len(patterns))
	for i, p := range patterns {
		parts := patternVariable.Split(p, -1)
		for j, part := range parts {
			parts[j] = regexp.QuoteMeta(part)
		}
		alternatives[i] = strings.Join(parts, "[^/]+")
	}
	if len(alternatives) == 1 {
		return "^" + alternatives[0] + "$"
	}
	return "^(?:" + strings.Join(alternatives, "|") + ")$"
}
