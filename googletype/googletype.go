// Package googletype documents the fields of the googleapis messages in
// google.type, such as PostalAddress, Date and Decimal: a realistic example
// of each and a description taken from its googleapis comment.
//
// A service cannot add protovalidate examples to those protos, so the
// generators fall back on these: the REST reference and the MCP tools show
// an example on every field of a google.type message, as HighLevel's public
// API quality gate requires. A description is a fallback too, for a plugin
// request that carries no comments for googleapis' files.
package googletype

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Field is what the package knows of a field of a google.type message.
type Field struct {
	// Example is a value of the field as proto3 JSON writes it: a 64-bit
	// integer as a decimal string, a Timestamp in RFC 3339, a Duration in
	// seconds with an "s" suffix, a list for a repeated field. It is nil for
	// a field of a message type, whose own fields carry examples.
	Example any
	// Description is the field's googleapis comment, shortened.
	Description string
}

// Lookup returns what the package knows of a field, by its full name, such
// as google.type.Date.year. The example is a value of the caller's own,
// which it may change.
func Lookup(name protoreflect.FullName) (Field, bool) {
	e, ok := fields[name]
	if !ok {
		return Field{}, false
	}
	out := Field{Description: e.description}
	if e.example != "" {
		// The examples are constants, written as JSON so each lookup
		// decodes a value of its own. One that is not JSON is a bug in
		// this package, which its tests catch.
		if err := json.Unmarshal([]byte(e.example), &out.Example); err != nil {
			panic(fmt.Sprintf("googletype: the example of %s is not JSON: %v", name, err))
		}
	}
	return out, true
}

// Names returns the full names of the fields the package documents, sorted.
func Names() []protoreflect.FullName {
	return slices.Sorted(maps.Keys(fields))
}

// entry is what fields holds for a field: its example as JSON, empty for a
// field of a message type, and its description.
type entry struct {
	example     string
	description string
}
