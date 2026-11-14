package restdoc

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pb33f/libopenapi"
)

const examplesProto = "acme/examples/v1/examples.proto"

// exampleDoc is the part of a generated document the example tests read.
type exampleDoc struct {
	Paths map[string]map[string]struct {
		Parameters []struct {
			Name   string `json:"name"`
			In     string `json:"in"`
			Schema struct {
				Example any `json:"example"`
			} `json:"schema"`
		} `json:"parameters"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Example    any `json:"example"`
			Properties map[string]struct {
				Example any `json:"example"`
				Items   *struct {
					Properties map[string]struct {
						Example any `json:"example"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func generateExamples(t *testing.T) (string, exampleDoc) {
	t.Helper()
	got := generate(t, "", examplesProto)
	var doc exampleDoc
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	return got, doc
}

// Every property carries the first protovalidate example of its field, in
// the JSON mapping its schema declares.
func TestGenerateFieldExamples(t *testing.T) {
	got, doc := generateExamples(t)
	props := doc.Components.Schemas["Widget"].Properties
	for name, want := range map[string]any{
		"name":       "shelves/main/widgets/w-42",
		"count":      "12", // proto3 JSON writes 64-bit integers as strings
		"onSale":     true,
		"weight":     1.5,
		"thumbnail":  "aGk=",
		"color":      "COLOR_BLUE",
		"finish":     "FINISH_MATTE", // the first value after the zero value
		"labels":     []any{"blue"},
		"parts":      []any{map[string]any{"code": "P-1"}}, // not inside itself
		"attributes": map[string]any{"height": float64(30)},
		"createTime": "2026-01-01T00:00:00Z",
		"lifetime":   "90s",
		"nickname":   "Widgy",
		"note":       nil,
		"size":       nil, // string rules on an int32 field: not an int32 example
		"mainPart":   nil, // a $ref: its schema documents its fields
	} {
		p, ok := props[name]
		if !ok {
			t.Errorf("Widget has no property %s", name)
			continue
		}
		if !reflect.DeepEqual(p.Example, want) {
			t.Errorf("Widget.%s example = %#v, want %#v", name, p.Example, want)
		}
	}
	if ex := doc.Components.Schemas["Widget"].Example; ex != nil {
		t.Errorf("Widget has an example of its own: %v", ex)
	}

	parsed, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
}

// Query parameters carry their field's example, and path parameters the
// segment of the bound resource name's example that they stand for.
func TestGenerateParameterExamples(t *testing.T) {
	_, doc := generateExamples(t)
	item := doc.Paths["/v1/shelves/{shelf}/widgets/{widget}"]
	for method, want := range map[string]map[string]any{
		"get": {
			"shelf":    "main",
			"widget":   "w-42",
			"pageSize": float64(25),
			"view":     "FINISH_MATTE",
			"tags":     []any{"new"},
			"readTime": "2026-01-01T00:00:00Z",
		},
		"patch": {
			"shelf":      "main",
			"widget":     "w-42",
			"updateMask": "onSale,count",
		},
	} {
		got := map[string]any{}
		for _, p := range item[method].Parameters {
			got[p.Name] = p.Schema.Example
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s parameter examples = %#v, want %#v", method, got, want)
		}
	}
}

// The error schema documents its own example.
func TestGenerateStatusExamples(t *testing.T) {
	_, doc := generateExamples(t)
	for name, prop := range doc.Components.Schemas[statusSchema].Properties {
		if prop.Example == nil {
			t.Errorf("Status.%s has no example", name)
		}
	}
	if ex := doc.Components.Schemas[statusSchema].Properties["details"].Items.Properties["@type"].Example; ex != errorInfoType {
		t.Errorf("Status.details[].@type example = %v", ex)
	}
}

func TestSegmentExample(t *testing.T) {
	for _, c := range []struct {
		full, name string
		example    any
		want       any
	}{
		{"shelves/{shelf}/books/{book}", "book", "shelves/1/books/123", "123"},
		{"shelves/{shelf}/books/{book}", "shelf", "shelves/1/books/123", "1"},
		{"shelves/{shelf}/books/{book}", "book", "shelves/1/books", nil},    // another form
		{"shelves/{shelf}/books/{book}", "book", "racks/1/books/123", nil},  // another collection
		{"shelves/{shelf}/books/{book}", "book", "shelves//books/123", nil}, // an empty segment
		{"shelves/{shelf}/books/{book}", "book", 7, nil},                    // not a string
		{"{name}", "name", "w-1", "w-1"},
	} {
		if got := segmentExample(c.full, c.name, c.example); got != c.want {
			t.Errorf("segmentExample(%q, %q, %v) = %v, want %v", c.full, c.name, c.example, got, c.want)
		}
	}
}
