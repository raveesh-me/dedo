package restdoc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/pb33f/libopenapi"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/raveesh-me/dedo/googletype"
)

const placesProto = "acme/places/v1/places.proto"

// googleTypeSchemas are the component schemas of the google.type messages
// the places example reaches, by the short names the document gives them.
var googleTypeSchemas = []string{
	"Color", "Date", "DateTime", "Decimal", "Expr", "Fraction", "Interval",
	"LatLng", "LocalizedText", "Money", "PhoneNumber", "PostalAddress",
	"Quaternion", "ShortCode", "TimeOfDay", "TimeZone",
}

func TestGeneratePlacesGolden(t *testing.T) {
	got := generate(t, "title=Places API,filename=places.openapi.json", placesProto)
	golden := filepath.Join("testdata", "places.openapi.json")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s; run go test ./restdoc -update and review the diff", golden)
	}
	doc, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
}

// Every property of a google.type schema and of the error schema has a
// description and an example, as HighLevel's public API quality gate
// requires (its rules 12 and 13); a reference to another schema needs no
// example, since that schema documents its fields. The descriptions come
// from the googleapis comments, and from dedo when a request carries none.
func TestGoogleTypePropertiesDocumented(t *testing.T) {
	for name, got := range map[string]string{
		"with comments":    generate(t, "", placesProto),
		"without comments": generateWithoutComments(t, "google/type/", placesProto),
	} {
		schemas := componentSchemas(t, got)
		for _, s := range append(googleTypeSchemas, statusSchema) {
			schema, ok := schemas[s].(map[string]any)
			if !ok {
				t.Errorf("%s: no component schema %s", name, s)
				continue
			}
			for _, problem := range undocumented(s, schema) {
				t.Errorf("%s: %s", name, problem)
			}
		}
	}
}

// The googleapis comment, when the request carries it, is the description:
// dedo's own is only the fallback.
func TestGoogleTypeCommentsPreferred(t *testing.T) {
	address := componentSchemas(t, generate(t, "", placesProto))["PostalAddress"].(map[string]any)
	region := address["properties"].(map[string]any)["regionCode"].(map[string]any)
	if d, _ := region["description"].(string); !strings.HasPrefix(d, "Required. CLDR region code of the country/region") {
		t.Errorf("regionCode description = %q, want the googleapis comment", d)
	}
}

// undocumented lists the properties of schema, however deeply nested, that
// lack a description, or an example when they are not a reference or an
// object with properties of its own.
func undocumented(path string, schema map[string]any) []string {
	var out []string
	props, _ := schema["properties"].(map[string]any)
	for name, v := range props {
		p, _ := v.(map[string]any)
		at := path + "." + name
		if d, _ := p["description"].(string); strings.TrimSpace(d) == "" {
			out = append(out, at+" has no description")
		}
		if _, ok := p["example"]; !ok && !objectLike(p) {
			out = append(out, at+" has no example")
		}
		out = append(out, undocumented(at, p)...)
	}
	if items, ok := schema["items"].(map[string]any); ok {
		out = append(out, undocumented(path+"[]", items)...)
	}
	return out
}

func objectLike(s map[string]any) bool {
	if _, ok := s["$ref"]; ok {
		return true
	}
	props, _ := s["properties"].(map[string]any)
	return s["type"] == "object" && len(props) > 0
}

// Every field of every google.type message has a description, and every
// one that is not a message of its own an example that its JSON mapping
// accepts, so the examples cannot drift from googleapis.
func TestGoogleTypeExamplesValid(t *testing.T) {
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{"testdata/deps"}}),
	}
	paths, err := filepath.Glob("testdata/deps/google/type/*.proto")
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range paths {
		paths[i] = strings.TrimPrefix(p, "testdata/deps/")
	}
	files, err := compiler.Compile(context.Background(), paths...)
	if err != nil {
		t.Fatal(err)
	}
	known := map[protoreflect.FullName]bool{}
	for _, f := range files {
		for _, md := range allMessages(f.Messages()) {
			for i := range md.Fields().Len() {
				fd := md.Fields().Get(i)
				known[fd.FullName()] = true
				checkGoogleTypeField(t, md, fd)
			}
		}
	}
	for _, name := range googletype.Names() {
		if !known[name] {
			t.Errorf("googletype documents %s, which google.type does not have", name)
		}
	}
}

func checkGoogleTypeField(t *testing.T, md protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor) {
	t.Helper()
	doc, ok := googletype.Lookup(fd.FullName())
	if !ok || strings.TrimSpace(doc.Description) == "" {
		t.Errorf("%s has no description", fd.FullName())
	}
	if fd.Message() != nil && !fd.IsList() && wellKnown(fd.Message()) == nil {
		if doc.Example != nil {
			t.Errorf("%s is a message, documented by its own schema, but has an example", fd.FullName())
		}
		return
	}
	if doc.Example == nil {
		t.Errorf("%s has no example", fd.FullName())
		return
	}
	b, err := json.Marshal(map[string]any{fd.JSONName(): doc.Example})
	if err != nil {
		t.Fatal(err)
	}
	if err := protojson.Unmarshal(b, dynamicpb.NewMessage(md)); err != nil {
		t.Errorf("%s example %s is not valid proto3 JSON: %v", fd.FullName(), b, err)
	}
}

func allMessages(msgs protoreflect.MessageDescriptors) []protoreflect.MessageDescriptor {
	var out []protoreflect.MessageDescriptor
	for i := range msgs.Len() {
		md := msgs.Get(i)
		out = append(out, md)
		out = append(out, allMessages(md.Messages())...)
	}
	return out
}

// The examples of the googleapis fields compose the example of a list of
// their messages, and a oneof contributes only its first member with an
// example: a phone number is a number or a short code, never both.
func TestGoogleTypeComposedExamples(t *testing.T) {
	site := componentSchemas(t, generate(t, "", placesProto))["Site"].(map[string]any)["properties"].(map[string]any)
	example := func(prop string) map[string]any {
		list, _ := site[prop].(map[string]any)["example"].([]any)
		if len(list) != 1 {
			t.Fatalf("Site.%s example = %v, want one item", prop, site[prop])
		}
		return list[0].(map[string]any)
	}
	address := example("previousAddresses")
	for field, want := range map[string]any{
		"regionCode":   "US",
		"postalCode":   "94043",
		"addressLines": []any{"1600 Amphitheatre Parkway"},
		"revision":     float64(0),
	} {
		if !reflect.DeepEqual(address[field], want) {
			t.Errorf("previousAddresses[0].%s = %#v, want %#v", field, address[field], want)
		}
	}
	phone := example("phones")
	if _, ok := phone["e164Number"]; !ok {
		t.Errorf("phones[0] = %v, want an e164Number", phone)
	}
	if _, ok := phone["shortCode"]; ok {
		t.Errorf("phones[0] = %v, sets both members of the oneof kind", phone)
	}
	// The first member has no example, so the next one stands for the oneof.
	if visit := example("visits"); !reflect.DeepEqual(visit, map[string]any{
		"day": map[string]any{"year": float64(2026), "month": float64(11), "day": float64(14)},
	}) {
		t.Errorf("visits[0] = %v, want only its day", visit)
	}
	inspection := example("inspections")
	if _, ok := inspection["utcOffset"]; !ok {
		t.Errorf("inspections[0] = %v, want a utcOffset", inspection)
	}
	if _, ok := inspection["timeZone"]; ok {
		t.Errorf("inspections[0] = %v, sets both members of the oneof time_offset", inspection)
	}
}

// A body or a response that is always the empty JSON object says so, as
// enum: [{}], rather than being an object schema without properties: the
// runtime requires the body, and answers 200 with {}, so neither leaving
// the body out nor a 204 would be true.
func TestEmptyBodiesAreExplicit(t *testing.T) {
	got := generate(t, "", placesProto)
	var doc struct {
		Paths map[string]map[string]struct {
			RequestBody *struct {
				Required bool `json:"required"`
				Content  map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	empty := []any{map[string]any{}}
	archive := doc.Paths["/v1/sites/{site}:archive"]["post"]
	if archive.RequestBody == nil || !archive.RequestBody.Required {
		t.Fatalf("ArchiveSite body = %+v, want a required one", archive.RequestBody)
	}
	if body := archive.RequestBody.Content["application/json"].Schema; !reflect.DeepEqual(body["enum"], empty) {
		t.Errorf("ArchiveSite body schema = %v, want enum [{}]", body)
	}
	if res := componentSchemas(t, got)["ArchiveSiteResponse"].(map[string]any); !reflect.DeepEqual(res["enum"], empty) {
		t.Errorf("ArchiveSiteResponse = %v, want enum [{}]", res)
	}
	del := doc.Paths["/v1/sites/{site}"]["delete"].Responses["200"].Content["application/json"].Schema
	if !reflect.DeepEqual(del["enum"], empty) {
		t.Errorf("DeleteSite 200 schema = %v, want enum [{}]", del)
	}
	get := doc.Paths["/v1/sites/{site}"]["get"].Responses["200"].Content["application/json"].Schema
	if _, ok := get["enum"]; ok {
		t.Errorf("GetSite 200 schema = %v, a site is not always empty", get)
	}
	// A body with fields the path leaves to it is not empty.
	rename := doc.Paths["/v1/sites/{site}:rename"]["post"].RequestBody.Content["application/json"].Schema
	if _, ok := rename["enum"]; ok || rename["properties"] == nil {
		t.Errorf("RenameSite body schema = %v, want its displayName and no enum", rename)
	}
}

// A google.protobuf.Any says what its @type is, with an example.
func TestAnyTypeDocumented(t *testing.T) {
	site := componentSchemas(t, generate(t, "", placesProto))["Site"].(map[string]any)["properties"].(map[string]any)
	typ := site["record"].(map[string]any)["properties"].(map[string]any)["@type"].(map[string]any)
	if d, _ := typ["description"].(string); d == "" {
		t.Error("Any.@type has no description")
	}
	if ex, _ := typ["example"].(string); !strings.HasPrefix(ex, "type.googleapis.com/") {
		t.Errorf("Any.@type example = %v, want a type URL", typ["example"])
	}
}

func componentSchemas(t *testing.T, doc string) map[string]any {
	t.Helper()
	var parsed struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Components.Schemas
}

// generateWithoutComments generates from a request whose files under
// prefix carry no source info, as some plugin hosts send dependencies.
func generateWithoutComments(t *testing.T, prefix string, protos ...string) string {
	t.Helper()
	req := request(t, "", protos...)
	for _, f := range req.GetProtoFile() {
		if strings.HasPrefix(f.GetName(), prefix) {
			f.SourceCodeInfo = nil
		}
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := Generate(plugin, Config{}); err != nil {
		t.Fatal(err)
	}
	return plugin.Response().File[0].GetContent()
}
