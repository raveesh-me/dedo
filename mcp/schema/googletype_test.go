package schema

import (
	"fmt"
	"strings"
	"testing"
)

const placesProto = "acme/places/v1/places.proto"

// Every field of a google.type message in a tool's schema has a
// description and, unless it is a message of its own, an example: the
// googleapis protos declare none, and a service cannot change them.
func TestGoogleTypeFieldsDocumented(t *testing.T) {
	site := Output(methodsOf(t, placesProto, "../../openapi/restdoc/testdata/domains")["GetSite"])
	compiled(t, "site", site)
	for _, field := range []string{
		"address", "open_date", "area", "rent", "position", "opens_at", "closure",
		"inspected_at", "color", "share", "orientation", "entry_rule", "sign",
	} {
		value := props(site, field)
		for name, v := range value["properties"].(Object) {
			p := v.(Object)
			if d, _ := p["description"].(string); d == "" {
				t.Errorf("%s.%s has no description", field, name)
			}
			if _, ok := p["examples"]; !ok && p["type"] != "object" {
				t.Errorf("%s.%s has no examples", field, name)
			}
		}
	}
	for path, want := range map[string]any{
		"address.region_code":     "US",
		"address.address_lines":   []any{"1600 Amphitheatre Parkway"},
		"open_date.year":          float64(2026),
		"area.value":              "12.50",
		"closure.start_time":      "2026-11-14T09:00:00Z",
		"inspected_at.utc_offset": "-18000s",
	} {
		field, name, _ := strings.Cut(path, ".")
		got, _ := props(site, field, name)["examples"].([]any)
		if len(got) != 1 || fmt.Sprint(got[0]) != fmt.Sprint(want) {
			t.Errorf("%s examples = %#v, want [%#v]", path, got, want)
		}
	}
}

// Without comments for googleapis' files, the google.type fields keep
// descriptions: dedo's own.
func TestGoogleTypeDescriptionsWithoutComments(t *testing.T) {
	req := requestOf(t, placesProto, "../../openapi/restdoc/testdata/domains")
	for _, f := range req.GetProtoFile() {
		if strings.HasPrefix(f.GetName(), "google/type/") {
			f.SourceCodeInfo = nil
		}
	}
	site := Output(methodsIn(t, req)["GetSite"])
	for name, v := range props(site, "address")["properties"].(Object) {
		if d, _ := v.(Object)["description"].(string); d == "" {
			t.Errorf("address.%s has no description", name)
		}
	}
	// Only the field's own comment is left: PostalAddress's is stripped.
	if d, _ := props(site, "address")["description"].(string); d != "Where it is." {
		t.Errorf("address description = %q, want only the field's comment", d)
	}
}

// A google.protobuf.Any says what its @type is.
func TestAnyTypeDescribed(t *testing.T) {
	site := Output(methodsOf(t, placesProto, "../../openapi/restdoc/testdata/domains")["GetSite"])
	typ := props(site, "record", "@type")
	if d, _ := typ["description"].(string); d == "" {
		t.Error("record.@type has no description")
	}
}
