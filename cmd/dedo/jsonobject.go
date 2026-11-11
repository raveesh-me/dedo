package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// member is one name and value of a JSON object, the value as written.
type member struct {
	Name  string
	Value json.RawMessage
}

// object is a JSON object that keeps its members in order, so a file dedo
// edits keeps the layout its owner gave it.
type object []member

// parseObject reads a JSON object. Empty input is an empty object.
func parseObject(b []byte) (object, error) {
	if t := bytes.TrimSpace(b); len(t) == 0 || string(t) == "null" {
		return object{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var out object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected %v", tok)
		}
		if _, dup := out.get(name); dup {
			// JSON.parse, as Claude Desktop reads it, takes the last one;
			// an edit of the first would do nothing.
			return nil, fmt.Errorf("%q appears twice", name)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, member{name, v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("data after the JSON object")
	}
	return out, nil
}

// get returns a member's value, if the object has it.
func (o object) get(name string) (json.RawMessage, bool) {
	i := slices.IndexFunc(o, func(m member) bool { return m.Name == name })
	if i < 0 {
		return nil, false
	}
	return o[i].Value, true
}

// with returns a copy of o with the member set: in its place if o has it,
// else last.
func (o object) with(name string, v json.RawMessage) object {
	out := slices.Clone(o)
	if i := slices.IndexFunc(out, func(m member) bool { return m.Name == name }); i >= 0 {
		out[i] = member{name, v}
		return out
	}
	return append(out, member{name, v})
}

// without returns a copy of o without the member.
func (o object) without(name string) object {
	return slices.DeleteFunc(slices.Clone(o), func(m member) bool { return m.Name == name })
}

// MarshalJSON writes the members in order.
func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		name, err := json.Marshal(m.Name)
		if err != nil {
			return nil, err
		}
		b.Write(name)
		b.WriteByte(':')
		b.Write(m.Value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// indent writes o as JSON indented by two spaces, ending in a newline.
func (o object) indent() ([]byte, error) {
	flat, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, flat, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}
