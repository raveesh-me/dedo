package restdoc

import (
	"fmt"
	"strings"
)

// segment is one part of a parsed google.api.http path template.
type segment struct {
	literal string // set for a literal segment
	field   string // set for a variable: the request field path it binds
	pattern []string
}

// template is a parsed path template such as "/v1/{name=todos/*}:remove".
type template struct {
	segments []segment
	verb     string
}

// parseTemplate parses the google.api.http path syntax:
//
//	Template = "/" Segments [ ":" Verb ]
//	Segment  = "*" | "**" | LITERAL | "{" FieldPath [ "=" Segments ] "}"
func parseTemplate(path string) (template, error) {
	if !strings.HasPrefix(path, "/") {
		return template{}, fmt.Errorf("path %q must start with /", path)
	}
	body, verb := splitVerb(path[1:])
	var t template
	t.verb = verb
	for rest := body; rest != ""; {
		if strings.HasPrefix(rest, "{") {
			end := strings.Index(rest, "}")
			if end < 0 {
				return template{}, fmt.Errorf("path %q has an unclosed variable", path)
			}
			t.segments = append(t.segments, parseVariable(rest[1:end]))
			rest = strings.TrimPrefix(rest[end+1:], "/")
			continue
		}
		lit, next, _ := strings.Cut(rest, "/")
		t.segments = append(t.segments, segment{literal: lit})
		rest = next
	}
	return t, nil
}

// splitVerb separates a trailing ":verb" that is outside any variable.
func splitVerb(body string) (string, string) {
	depth := 0
	for i := len(body) - 1; i >= 0; i-- {
		switch body[i] {
		case '}':
			depth++
		case '{':
			depth--
		case '/':
			if depth == 0 {
				return body, ""
			}
		case ':':
			if depth == 0 {
				return body[:i], body[i+1:]
			}
		}
	}
	return body, ""
}

func parseVariable(v string) segment {
	field, pattern, ok := strings.Cut(v, "=")
	if !ok {
		return segment{field: field, pattern: []string{"*"}}
	}
	return segment{field: field, pattern: strings.Split(pattern, "/")}
}

// pathParam is one OpenAPI path parameter and the field it fills.
type pathParam struct {
	name  string
	field string
	full  string // the field's value template, such as "todos/{todo}"
}

// openAPIPath renders the template with one OpenAPI parameter per wildcard.
// names supplies the parameter names for a variable's wildcards, usually
// from the matching resource pattern; fallbacks are derived from the
// preceding literal.
func (t template) openAPIPath(names func(seg segment) []string) (string, []pathParam) {
	var parts []string
	var params []pathParam
	for _, seg := range t.segments {
		if seg.field == "" {
			parts = append(parts, seg.literal)
			continue
		}
		given := names(seg)
		var full []string
		wildcard := 0
		for i, p := range seg.pattern {
			if p != "*" && p != "**" {
				parts = append(parts, p)
				full = append(full, p)
				continue
			}
			name := fallbackName(seg, i)
			if wildcard < len(given) {
				name = given[wildcard]
			}
			wildcard++
			parts = append(parts, "{"+name+"}")
			full = append(full, "{"+name+"}")
			params = append(params, pathParam{name: name, field: seg.field})
		}
		for i := range params {
			if params[i].field == seg.field {
				params[i].full = strings.Join(full, "/")
			}
		}
	}
	path := "/" + strings.Join(parts, "/")
	if t.verb != "" {
		path += ":" + t.verb
	}
	return path, params
}

// fallbackName names a wildcard that matches no resource pattern after the
// collection before it, without guessing a singular.
func fallbackName(seg segment, i int) string {
	if i > 0 {
		return seg.pattern[i-1] + "Id"
	}
	parts := strings.Split(seg.field, ".")
	return parts[len(parts)-1]
}

// literals returns the literal segments of a pattern, which identify the
// resource pattern it matches.
func literals(pattern []string) []string {
	var out []string
	for _, p := range pattern {
		if p != "*" && p != "**" && !strings.HasPrefix(p, "{") {
			out = append(out, p)
		}
	}
	return out
}
