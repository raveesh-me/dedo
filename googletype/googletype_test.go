package googletype

import (
	"reflect"
	"slices"
	"testing"
)

func TestLookup(t *testing.T) {
	doc, ok := Lookup("google.type.PostalAddress.address_lines")
	if !ok {
		t.Fatal("no google.type.PostalAddress.address_lines")
	}
	if want := []any{"1600 Amphitheatre Parkway"}; !reflect.DeepEqual(doc.Example, want) {
		t.Errorf("example = %#v, want %#v", doc.Example, want)
	}
	if doc.Description == "" {
		t.Error("no description")
	}
	if _, ok := Lookup("google.type.PostalAddress.planet"); ok {
		t.Error("found a field google.type does not have")
	}
}

// Each lookup returns a value of its own, so a caller that changes one
// changes no other caller's.
func TestLookupReturnsCopies(t *testing.T) {
	first, _ := Lookup("google.type.PostalAddress.address_lines")
	first.Example.([]any)[0] = "changed"
	second, _ := Lookup("google.type.PostalAddress.address_lines")
	if got := second.Example.([]any)[0]; got != "1600 Amphitheatre Parkway" {
		t.Errorf("second lookup = %v, changed by the first caller", got)
	}
}

// A message field has a description but no example: its own fields have
// theirs.
func TestMessageFieldHasNoExample(t *testing.T) {
	doc, ok := Lookup("google.type.DateTime.time_zone")
	if !ok || doc.Description == "" {
		t.Fatalf("google.type.DateTime.time_zone = %+v, %v", doc, ok)
	}
	if doc.Example != nil {
		t.Errorf("example = %#v, want none", doc.Example)
	}
}

func TestNamesSorted(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no names")
	}
	if !slices.IsSorted(names) {
		t.Error("names are not sorted")
	}
}

// Every example is JSON, so no lookup panics.
func TestEveryExampleDecodes(t *testing.T) {
	for _, name := range Names() {
		if _, ok := Lookup(name); !ok {
			t.Errorf("%s: no entry", name)
		}
	}
}
