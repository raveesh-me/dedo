package restdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
)

// A header file lists the request headers every operation declares, which
// the consumer's platform requires and the protos cannot say, such as a
// gateway's Version header. dedo assumes none. Each entry is an OpenAPI
// parameter object without its "in":
//
//	{
//	  "headers": [
//	    {
//	      "name": "Version",
//	      "description": "API Version. Send `v4`.",
//	      "required": true,
//	      "schema": {"type": "string", "enum": ["v4"], "example": "v4"},
//	      "example": "v4"
//	    }
//	  ]
//	}
//
// name, description and schema.type are required; required, schema.enum,
// schema.example and example are optional.

type headerFile struct {
	Headers []headerEntry `json:"headers"`
}

type headerEntry struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	Schema      *headerSchema `json:"schema"`
	Example     any           `json:"example"`
}

type headerSchema struct {
	Type    string `json:"type"`
	Enum    []any  `json:"enum"`
	Example any    `json:"example"`
}

// headerTypes are the schema types a header's value may have.
var headerTypes = []string{"string", "integer", "number", "boolean"}

// ignoredHeaders are the header parameters OpenAPI says to ignore: the
// request body's media type, the response's, and the security scheme's.
var ignoredHeaders = []string{"Accept", "Content-Type", "Authorization"}

// readHeaders reads the header files, in order, into the header parameters
// every operation declares, and refuses one header name twice, ignoring
// case, as HTTP does.
func readHeaders(paths []string) ([]Parameter, error) {
	var out []Parameter
	for _, path := range paths {
		params, err := readHeaderFile(path)
		if err != nil {
			return nil, fmt.Errorf("headers: %s: %w", path, err)
		}
		for _, p := range params {
			if i := slices.IndexFunc(out, func(q Parameter) bool { return strings.EqualFold(q.Name, p.Name) }); i >= 0 {
				return nil, fmt.Errorf("headers: %s: header %s twice, with %s", path, p.Name, out[i].Name)
			}
			out = append(out, p)
		}
	}
	return out, nil
}

func readHeaderFile(path string) ([]Parameter, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var f headerFile
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("data after the headers")
	}
	if len(f.Headers) == 0 {
		return nil, errors.New(`lists no header: give "headers" at least one`)
	}
	out := make([]Parameter, 0, len(f.Headers))
	for i, h := range f.Headers {
		p, err := h.parameter()
		if err != nil {
			return nil, fmt.Errorf("header %d: %w", i+1, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// parameter checks the entry and returns its header parameter.
func (h headerEntry) parameter() (Parameter, error) {
	if err := h.checkName(); err != nil {
		return Parameter{}, err
	}
	if strings.TrimSpace(h.Description) == "" {
		return Parameter{}, fmt.Errorf("%s has no description", h.Name)
	}
	if h.Schema == nil || h.Schema.Type == "" {
		return Parameter{}, fmt.Errorf("%s has no schema type: give one of %s", h.Name, strings.Join(headerTypes, ", "))
	}
	if err := h.Schema.check(h.Example); err != nil {
		return Parameter{}, fmt.Errorf("%s: %w", h.Name, err)
	}
	return Parameter{
		Name:        h.Name,
		In:          "header",
		Description: h.Description,
		Required:    h.Required,
		Schema:      &Schema{Type: h.Schema.Type, Enum: h.Schema.Enum, Example: h.Schema.Example},
		Example:     h.Example,
	}, nil
}

func (h headerEntry) checkName() error {
	if h.Name == "" {
		return errors.New("has no name")
	}
	if !isToken(h.Name) {
		return fmt.Errorf("%q is not a header name: use letters, digits and !#$%%&'*+-.^_`|~ only (RFC 9110)", h.Name)
	}
	for _, ignored := range ignoredHeaders {
		if strings.EqualFold(h.Name, ignored) {
			return fmt.Errorf("%s is a header OpenAPI ignores as a parameter (Accept, Content-Type and Authorization are)", h.Name)
		}
	}
	return nil
}

// check refuses a type a header cannot have, an empty enum, and an enum
// value or example that is not of the type or, for an example, not one of
// the enum's values.
func (s *headerSchema) check(paramExample any) error {
	if !slices.Contains(headerTypes, s.Type) {
		return fmt.Errorf("type %q: give one of %s", s.Type, strings.Join(headerTypes, ", "))
	}
	if s.Enum != nil && len(s.Enum) == 0 {
		return errors.New("an empty enum admits no value: leave enum out or list the values")
	}
	for _, v := range s.Enum {
		if !ofType(v, s.Type) {
			return fmt.Errorf("enum value %s is not of type %s", show(v), s.Type)
		}
	}
	for _, example := range []any{s.Example, paramExample} {
		if example == nil {
			continue
		}
		if !ofType(example, s.Type) {
			return fmt.Errorf("example %s is not of type %s", show(example), s.Type)
		}
		if s.Enum != nil && !slices.ContainsFunc(s.Enum, func(v any) bool { return sameValue(v, example) }) {
			return fmt.Errorf("example %s is not one of the enum's values", show(example))
		}
	}
	return nil
}

// withHeaders returns the operation's path and query parameters followed
// by the headers, and refuses a header named like one of them, ignoring
// case, which would leave a caller, or a client generated from the
// document, two inputs of one name.
func withHeaders(params, headers []Parameter) ([]Parameter, error) {
	for _, h := range headers {
		for _, p := range params {
			if strings.EqualFold(h.Name, p.Name) {
				return nil, fmt.Errorf("header %s has the name of its %s parameter %s: rename the header", h.Name, p.In, p.Name)
			}
		}
	}
	return slices.Concat(params, headers), nil
}

// isToken reports whether s is an HTTP field name: one or more tchars.
func isToken(s string) bool {
	for _, c := range s {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)
		if !ok {
			return false
		}
	}
	return s != ""
}

// ofType reports whether a value decoded with UseNumber is of the JSON
// Schema type.
func ofType(v any, typ string) bool {
	switch typ {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f == math.Trunc(f)
	}
	return false
}

// sameValue compares two values decoded with UseNumber, numbers by value,
// so 1 and 1.0 are the same.
func sameValue(a, b any) bool {
	na, aNum := a.(json.Number)
	nb, bNum := b.(json.Number)
	if aNum && bNum {
		fa, errA := na.Float64()
		fb, errB := nb.Float64()
		return errA == nil && errB == nil && fa == fb
	}
	return a == b
}

func show(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
