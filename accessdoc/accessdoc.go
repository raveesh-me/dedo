// Package accessdoc reads access requirements per scope: what an auth
// generator says a caller holding a scope must also satisfy, such as "only
// for agencies on the Founders plan", so that dedo's REST reference and MCP
// tools say it without the contract declaring it twice. dedo knows nothing
// of what a requirement means. An RPC gets the requirements of the scopes
// in its (dedo.v1.method).scopes: their paragraphs in its description, their
// extensions on its OpenAPI operation, and their meta in its MCP tool's
// _meta. Error reasons are not part of it: the refusal an RPC returns is in
// its (dedo.v1.method).errors, as every other.
//
// An access file is JSON, keyed by scope:
//
//	{
//	  "scopes": {
//	    "taxes/tariff-codes.readonly": {
//	      "description": "Only for: Agencies on the Founders plan (`founders`).",
//	      "extensions": {"x-commerce-conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}]},
//	      "meta": {"conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}]}
//	    }
//	  }
//	}
//
// Every field of an entry is optional. protoc-gen-dedo-openapi and
// protoc-gen-dedo-mcp read access files with their access=<path>
// parameter, once per file; the generator that writes them runs first.
package accessdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ExtensionPrefix starts every extension's name, as OpenAPI requires of a
// specification extension.
const ExtensionPrefix = "x-"

// Requirement is what the access files say of one scope, or, from For, of
// one RPC.
type Requirement struct {
	// Description is a Markdown paragraph, or several, for the people and
	// models choosing between operations. Empty when there is none.
	Description string
	// Extensions are OpenAPI specification extensions for the operation,
	// by name, each starting with x-, as raw JSON.
	Extensions map[string]json.RawMessage
	// Meta are entries of the MCP tool's _meta, by key, as raw JSON.
	Meta map[string]json.RawMessage
}

// Requirements are what the access files say, by scope.
type Requirements map[string]Requirement

// file is an access file as written.
type file struct {
	Scopes map[string]struct {
		Description string                     `json:"description"`
		Extensions  map[string]json.RawMessage `json:"extensions"`
		Meta        map[string]json.RawMessage `json:"meta"`
	} `json:"scopes"`
}

// Read reads the access files at paths, in order, and merges them: a
// scope's paragraphs are joined in the order of the files, and a scope may
// get each extension and each meta key once. A file that cannot be read or
// decoded, a field the format does not have, an entry without a scope, an
// extension whose name does not start with x- and an empty meta key are
// errors.
func Read(paths []string) (Requirements, error) {
	reqs := Requirements{}
	for _, path := range paths {
		f, err := parse(path)
		if err != nil {
			return nil, err
		}
		for _, scope := range sortedKeys(f.Scopes) {
			entry := f.Scopes[scope]
			name := strings.TrimSpace(scope)
			if name == "" {
				return nil, fmt.Errorf("accessdoc: %s: an entry names no scope", path)
			}
			for key := range entry.Extensions {
				if !strings.HasPrefix(key, ExtensionPrefix) {
					return nil, fmt.Errorf("accessdoc: %s: %s: extension %q does not start with %s", path, name, key, ExtensionPrefix)
				}
			}
			if _, empty := entry.Meta[""]; empty {
				return nil, fmt.Errorf("accessdoc: %s: %s: an empty meta key", path, name)
			}
			merged, err := merge(reqs[name], Requirement{Description: entry.Description, Extensions: entry.Extensions, Meta: entry.Meta})
			if err != nil {
				return nil, fmt.Errorf("accessdoc: %s: scope %s: %w", path, name, err)
			}
			reqs[name] = merged
		}
	}
	return reqs, nil
}

// For returns the requirements of an RPC declaring scopes: each scope's, in
// scope order. A scope no access file names adds nothing. An extension or
// a meta key that two of its scopes give is an error.
func (r Requirements) For(scopes []string) (Requirement, error) {
	var out Requirement
	for _, s := range scopes {
		req, ok := r[s]
		if !ok {
			continue
		}
		merged, err := merge(out, req)
		if err != nil {
			return Requirement{}, fmt.Errorf("accessdoc: scopes %s: %w", strings.Join(scopes, ", "), err)
		}
		out = merged
	}
	return out, nil
}

// MetaJSON is Meta as one compact JSON object with its keys sorted, or nil
// when there is none: the same requirement is always the same bytes.
func (r Requirement) MetaJSON() ([]byte, error) {
	if len(r.Meta) == 0 {
		return nil, nil
	}
	return json.Marshal(r.Meta)
}

// merge adds b to a: b's paragraphs after a's, and b's extensions and meta,
// none of which a may have. a is left as it was.
func merge(a, b Requirement) (Requirement, error) {
	out := Requirement{Description: strings.TrimSpace(a.Description)}
	if d := strings.TrimSpace(b.Description); d != "" {
		out.Description = strings.TrimSpace(out.Description + "\n\n" + d)
	}
	var err error
	if out.Extensions, err = union("extension", a.Extensions, b.Extensions); err != nil {
		return Requirement{}, err
	}
	if out.Meta, err = union("meta", a.Meta, b.Meta); err != nil {
		return Requirement{}, err
	}
	return out, nil
}

// union is a new map holding a's and b's entries, which must not share a
// key; nil when both are empty.
func union(kind string, a, b map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if len(a)+len(b) == 0 {
		return nil, nil
	}
	out := make(map[string]json.RawMessage, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for _, k := range sortedKeys(b) {
		if _, twice := out[k]; twice {
			return nil, fmt.Errorf("%s %q twice", kind, k)
		}
		out[k] = b[k]
	}
	return out, nil
}

// parse decodes one access file, refusing fields the format does not have.
func parse(path string) (file, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return file{}, fmt.Errorf("accessdoc: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return file{}, fmt.Errorf("accessdoc: %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return file{}, fmt.Errorf("accessdoc: %s: data after the requirements", path)
	}
	return f, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
