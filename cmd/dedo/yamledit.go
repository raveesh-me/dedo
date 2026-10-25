package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadYAML parses a YAML file into a node tree, which keeps its comments.
// It reports false when the file does not exist.
func loadYAML(path string) (*yaml.Node, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	doc, err := parseYAML(b)
	return doc, err == nil, err
}

func parseYAML(b []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("not a YAML mapping")
	}
	return &doc, nil
}

func encodeYAML(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

func root(doc *yaml.Node) *yaml.Node { return doc.Content[0] }

// lookup returns the value of key in a mapping, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// set adds key with value to a mapping, or replaces its value.
func set(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, scalar(key), value)
}

// child returns the value of key, creating it with the given kind.
func child(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	if v := lookup(m, key); v != nil && v.Kind == kind {
		return v
	}
	v := &yaml.Node{Kind: kind}
	if kind == yaml.MappingNode {
		v.Tag = "!!map"
	} else {
		v.Tag = "!!seq"
	}
	set(m, key, v)
	return v
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// scalars returns the values of a sequence of scalars.
func scalars(seq *yaml.Node) []string {
	var out []string
	if seq == nil {
		return out
	}
	for _, n := range seq.Content {
		if n.Kind == yaml.ScalarNode {
			out = append(out, n.Value)
		}
	}
	return out
}

// addScalars appends the values a sequence lacks and returns them.
func addScalars(seq *yaml.Node, values ...string) []string {
	have := scalars(seq)
	var added []string
	for _, v := range values {
		if !slices.Contains(have, v) {
			seq.Content = append(seq.Content, scalar(v))
			have = append(have, v)
			added = append(added, v)
		}
	}
	return added
}

// dropScalars removes the given values from a sequence and returns the
// ones it removed. A comment above a removed value goes with it, unless it
// is the first one, under the key: that one may describe the whole list, so
// it moves to the next value kept.
func dropScalars(seq *yaml.Node, values ...string) []string {
	if seq == nil {
		return nil
	}
	var dropped []string
	var kept []*yaml.Node
	header := ""
	for i, n := range seq.Content {
		if n.Kind == yaml.ScalarNode && slices.Contains(values, n.Value) {
			dropped = append(dropped, n.Value)
			if i == 0 {
				header = firstLine(n.HeadComment)
			}
			continue
		}
		if header != "" {
			n.HeadComment = strings.TrimSpace(header + "\n" + n.HeadComment)
			header = ""
		}
		kept = append(kept, n)
	}
	seq.Content = kept
	return dropped
}

// firstLine returns the first line of a comment block: a header above a list
// is a comment line of its own, followed by one about the first item.
func firstLine(comment string) string {
	if first, rest, ok := strings.Cut(comment, "\n"); ok && rest != "" {
		return first
	}
	return ""
}

// findItem returns the first mapping in a sequence that match accepts.
func findItem(seq *yaml.Node, match func(*yaml.Node) bool) *yaml.Node {
	if seq == nil {
		return nil
	}
	for _, n := range seq.Content {
		if n.Kind == yaml.MappingNode && match(n) {
			return n
		}
	}
	return nil
}

// allStrings returns every scalar value under n, in order.
func allStrings(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	for _, c := range n.Content {
		out = append(out, allStrings(c)...)
	}
	return out
}

// remove deletes key from a mapping.
func remove(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i:i], m.Content[i+2:]...)
			return
		}
	}
}

// cloneNode deep-copies a node, so a template entry can be edited without
// changing the template.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}
