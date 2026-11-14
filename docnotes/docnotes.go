// Package docnotes reads doc notes: what another generator adds to the REST
// reference and the MCP tools dedo's generators write for given RPCs, such
// as an auth generator saying which accounts a method is for. dedo knows
// nothing of what a note means; it puts the note's paragraph in the
// operation's and the tool's description, and its extensions on the
// operation.
//
// A notes file is JSON:
//
//	{
//	  "methods": {
//	    "acme.orders.v1.OrderService.RefundOrder": {
//	      "description": "Only for accounts on the pro plan.",
//	      "extensions": {"x-plans": {"plans": ["pro"]}}
//	    }
//	  }
//	}
//
// keyed by each RPC's full name. protoc-gen-dedo-openapi and
// protoc-gen-dedo-mcp read notes files with their notes=<path> parameter,
// once per file; the generator that writes them runs first.
package docnotes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ExtensionPrefix starts every extension's name, as OpenAPI requires of a
// specification extension.
const ExtensionPrefix = "x-"

// Note is what the notes files say of one RPC.
type Note struct {
	// Description is a Markdown paragraph, or several, for the people and
	// models choosing between operations: the files' paragraphs, in the
	// order of the files. Empty when they give none.
	Description string
	// Extensions are the OpenAPI specification extensions the operation
	// carries, by name, each starting with x-, as raw JSON.
	Extensions map[string]json.RawMessage
}

// Notes are the notes of every RPC the files name, by full name, such as
// acme.orders.v1.OrderService.RefundOrder.
type Notes map[string]Note

// file is a notes file as written.
type file struct {
	Methods map[string]struct {
		Description string                     `json:"description"`
		Extensions  map[string]json.RawMessage `json:"extensions"`
	} `json:"methods"`
}

// Read reads the notes files at paths, in order, and merges them: an RPC's
// paragraphs are joined in the order of the files, and an extension may be
// given once per RPC. A file that cannot be read or decoded, a field the
// format does not have, an RPC without a name and an extension whose name
// does not start with x- are errors.
func Read(paths []string) (Notes, error) {
	notes := Notes{}
	for _, path := range paths {
		f, err := parse(path)
		if err != nil {
			return nil, err
		}
		for name, m := range f.Methods {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, fmt.Errorf("docnotes: %s: a note names no method", path)
			}
			note := notes[name]
			if d := strings.TrimSpace(m.Description); d != "" {
				note.Description = strings.TrimSpace(note.Description + "\n\n" + d)
			}
			for key, value := range m.Extensions {
				if !strings.HasPrefix(key, ExtensionPrefix) {
					return nil, fmt.Errorf("docnotes: %s: %s: extension %q does not start with %s", path, name, key, ExtensionPrefix)
				}
				if _, twice := note.Extensions[key]; twice {
					return nil, fmt.Errorf("docnotes: %s: %s: the notes give extension %s twice", path, name, key)
				}
				if note.Extensions == nil {
					note.Extensions = map[string]json.RawMessage{}
				}
				note.Extensions[key] = value
			}
			notes[name] = note
		}
	}
	return notes, nil
}

// parse decodes one notes file, refusing fields the format does not have.
func parse(path string) (file, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return file{}, fmt.Errorf("docnotes: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return file{}, fmt.Errorf("docnotes: %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return file{}, fmt.Errorf("docnotes: %s: data after the notes", path)
	}
	return f, nil
}
