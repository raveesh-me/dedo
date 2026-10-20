package mcpserver

import (
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const fieldMaskName = "google.protobuf.FieldMask"

// normalizeMasks rewrites the field mask paths in a tool call's arguments to
// the lowerCamelCase the proto3 JSON mapping requires, so a model can name
// fields the way the input schema does (snake_case). Paths already in
// lowerCamelCase are left as they are.
func normalizeMasks(md protoreflect.MessageDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if !hasFieldMask(md, map[protoreflect.FullName]bool{}) {
		return args, nil
	}
	var v map[string]any
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	return json.Marshal(rewriteMasks(md, v))
}

// rewriteMasks returns a copy of v with every field mask's paths converted.
func rewriteMasks(md protoreflect.MessageDescriptor, v map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for key, value := range v {
		out[key] = value
		f := md.Fields().ByName(protoreflect.Name(key))
		if f == nil {
			f = md.Fields().ByJSONName(key)
		}
		if f == nil || f.Message() == nil || f.IsMap() {
			continue
		}
		switch value := value.(type) {
		case string:
			if f.Message().FullName() == fieldMaskName {
				out[key] = camelPaths(value)
			}
		case map[string]any:
			out[key] = rewriteMasks(f.Message(), value)
		case []any:
			items := make([]any, len(value))
			for i, item := range value {
				items[i] = item
				if m, ok := item.(map[string]any); ok {
					items[i] = rewriteMasks(f.Message(), m)
				}
			}
			out[key] = items
		}
	}
	return out
}

// camelPaths converts "budget_amount,todo.assignee_email" to
// "budgetAmount,todo.assigneeEmail".
func camelPaths(mask string) string {
	paths := strings.Split(mask, ",")
	for i, path := range paths {
		segments := strings.Split(strings.TrimSpace(path), ".")
		for j, s := range segments {
			segments[j] = lowerCamel(s)
		}
		paths[i] = strings.Join(segments, ".")
	}
	return strings.Join(paths, ",")
}

// lowerCamel mirrors protoc's JSON name: drop each underscore and upper-case
// the letter after it.
func lowerCamel(s string) string {
	var b strings.Builder
	upper := false
	for _, r := range s {
		switch {
		case r == '_':
			upper = true
		case upper:
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func hasFieldMask(md protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) bool {
	if seen[md.FullName()] {
		return false
	}
	seen[md.FullName()] = true
	fields := md.Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		if f.Message() == nil || f.IsMap() {
			continue
		}
		if f.Message().FullName() == fieldMaskName || hasFieldMask(f.Message(), seen) {
			return true
		}
	}
	return false
}
