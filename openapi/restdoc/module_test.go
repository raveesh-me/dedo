package restdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The shop fixture spans two packages with services and one of shared
// value objects without any.
var shopProtos = []string{
	"acme/catalog/v1/catalog.proto",
	"acme/orders/v1/orders.proto",
	"acme/type/type.proto",
}

// A package reads as its last segment before the version.
func TestModuleTitle(t *testing.T) {
	for _, c := range []struct {
		pkg  protoreflect.FullName
		want string
	}{
		{"taxes.content.v1", "Content"},
		{"shop.tax_profile.v1", "Tax profile"},
		{"acme.orders.v2beta1", "Orders"},
		{"acme.orders.v1alpha", "Orders"},
		{"acme.orders.v1p1beta1", "Orders"},
		{"acme.orders.v1test", "Orders"},
		{"acme.orders.version", "Version"},
		{"acme.type", "Type"},
		{"orders", "Orders"},
		{"v1", "V1"},
	} {
		if got := moduleTitle(c.pkg); got != c.want {
			t.Errorf("moduleTitle(%q) = %q, want %q", c.pkg, got, c.want)
		}
	}
}

// A document that spans packages nests each service's tag under a
// navigation tag for its package, named after the package and described by
// its comment. A package without services gets no tag.
func TestGenerateNestsServicesUnderPackages(t *testing.T) {
	got := generate(t, "title=Shop API,filename=shop.openapi.json", shopProtos...)
	golden := filepath.Join("testdata", "shop.openapi.json")
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

	var doc struct {
		OpenAPI string `json:"openapi"`
		Tags    []Tag  `json:"tags"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	wantTags := []Tag{
		{Name: "acme.catalog.v1", Summary: "Catalog", Description: "Catalog is what the shop sells.", Kind: "nav"},
		{Name: "Product", Description: "Reads the products on sale.", Parent: "acme.catalog.v1"},
		{Name: "acme.orders.v1", Summary: "Orders", Description: "Orders is what buyers commit to buy, and what they send back.", Kind: "nav"},
		{Name: "Orders", Description: "Reads orders. Its title is the package's title too: the package's tag is\nnamed after the package, so the two cannot clash.", Parent: "acme.orders.v1"},
		{Name: "Return", Description: "Reads returns.", Parent: "acme.orders.v1"},
	}
	if doc.OpenAPI != "3.2.0" {
		t.Errorf("openapi = %q, want 3.2.0", doc.OpenAPI)
	}
	if len(doc.Tags) != len(wantTags) {
		t.Fatalf("tags = %+v, want %+v", doc.Tags, wantTags)
	}
	for i := range wantTags {
		if doc.Tags[i] != wantTags[i] {
			t.Errorf("tag %d = %+v, want %+v", i, doc.Tags[i], wantTags[i])
		}
	}

	parsed, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
}

// A document whose services share one package keeps one flat tag per
// service: a single group would only add a level to the navigation.
func TestGenerateOnePackageHasNoPackageTag(t *testing.T) {
	var doc struct {
		Tags []Tag `json:"tags"`
	}
	if err := json.Unmarshal([]byte(generate(t, "")), &doc); err != nil {
		t.Fatal(err)
	}
	for _, tag := range doc.Tags {
		if tag.Kind != "" || tag.Parent != "" {
			t.Errorf("tag %+v nests, but the library has one package", tag)
		}
	}
}

// Operations name their tag by name alone, so two services of different
// packages with one title would land in one group: generation fails and
// names both.
func TestGenerateRejectsDuplicateTagNames(t *testing.T) {
	_, err := generatePlugin(t, "", "acme/orders/v1/orders.proto", "acme/billing/v1/billing.proto")
	if err == nil {
		t.Fatal("want an error for two tags named Orders")
	}
	for _, want := range []string{`"Orders"`, "acme.orders.v1", "acme.billing.v1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

// A service without a package cannot be grouped with other packages'.
func TestGenerateRejectsPackagelessServiceAmongPackages(t *testing.T) {
	_, err := generatePlugin(t, "", "acme/catalog/v1/catalog.proto", "nopackage.proto")
	if err == nil || !strings.Contains(err.Error(), `"Ping"`) {
		t.Fatalf("err = %v, want one naming the Ping service", err)
	}
}

func TestTagPlace(t *testing.T) {
	for _, c := range []struct {
		tag  Tag
		want string
	}{
		{Tag{Name: "acme.orders.v1", Kind: "nav"}, "package acme.orders.v1"},
		{Tag{Name: "Orders", Parent: "acme.orders.v1"}, "a service of package acme.orders.v1"},
		{Tag{Name: "Orders"}, "a service"},
	} {
		if got := tagPlace(c.tag); got != c.want {
			t.Errorf("tagPlace(%+v) = %q, want %q", c.tag, got, c.want)
		}
	}
}

// The document groups its tags in x-tagGroups, one group per package named
// after its title, so sites that build their navigation from the extension
// put every operation under its package rather than under "UNTAGGED".
func TestGenerateTagGroups(t *testing.T) {
	for _, c := range []struct {
		name   string
		params string
		protos []string
		want   []TagGroup
	}{
		{
			name:   "packages",
			protos: shopProtos,
			want: []TagGroup{
				{Name: "Catalog", Tags: []string{"Product"}},
				{Name: "Orders", Tags: []string{"Orders", "Return"}},
			},
		},
		{
			name: "one package",
			want: []TagGroup{{Name: "Library", Tags: []string{"Library"}}},
		},
		{
			name:   "no package",
			params: "title=Ping API",
			protos: []string{"nopackage.proto"},
			want:   []TagGroup{{Name: "Ping API", Tags: []string{"Ping"}}},
		},
		{name: "off", params: "no_tag_groups", protos: shopProtos},
		{name: "off with a value", params: "no_tag_groups=true", protos: shopProtos},
		{
			name:   "kept",
			params: "no_tag_groups=false",
			want:   []TagGroup{{Name: "Library", Tags: []string{"Library"}}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := generate(t, c.params, c.protos...)
			var doc struct {
				TagGroups []TagGroup `json:"x-tagGroups"`
			}
			if err := json.Unmarshal([]byte(got), &doc); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(doc.TagGroups, c.want) {
				t.Errorf("x-tagGroups = %+v, want %+v", doc.TagGroups, c.want)
			}
			if c.want == nil && strings.Contains(got, "x-tagGroups") {
				t.Error("the document has x-tagGroups")
			}
		})
	}
}

// Two packages with one title are told apart by their full names; the
// others keep their titles.
func TestTagGroupsSameTitle(t *testing.T) {
	mods := modules{
		{pkg: "acme.orders.v1", services: []Tag{{Name: "Orders"}}},
		{pkg: "shop.orders.v1", services: []Tag{{Name: "Shop orders"}}},
		{pkg: "acme.catalog.v1", services: []Tag{{Name: "Product"}, {Name: "Price"}}},
	}
	want := []TagGroup{
		{Name: "acme.orders.v1", Tags: []string{"Orders"}},
		{Name: "shop.orders.v1", Tags: []string{"Shop orders"}},
		{Name: "Catalog", Tags: []string{"Product", "Price"}},
	}
	if got := mods.tagGroups("API"); !reflect.DeepEqual(got, want) {
		t.Errorf("tagGroups = %+v, want %+v", got, want)
	}
}

func TestConfigNoTagGroups(t *testing.T) {
	for value, want := range map[string]bool{"": true, "true": true, "1": true, "false": false} {
		var cfg Config
		if err := cfg.Set("no_tag_groups", value); err != nil {
			t.Fatalf("no_tag_groups=%s: %v", value, err)
		}
		if cfg.NoTagGroups != want {
			t.Errorf("no_tag_groups=%s: NoTagGroups = %v, want %v", value, cfg.NoTagGroups, want)
		}
	}
	var cfg Config
	if err := cfg.Set("no_tag_groups", "yes"); err == nil || !strings.Contains(err.Error(), "no_tag_groups") {
		t.Errorf("no_tag_groups=yes: err = %v, want one naming the parameter", err)
	}
}

// x-tagGroups sits beside what other parameters add: access requirements
// on the operations and examples on the schemas.
func TestGenerateTagGroupsWithAccessAndExamples(t *testing.T) {
	got := generate(t, "access="+libraryAccess)
	var doc struct {
		TagGroups []TagGroup `json:"x-tagGroups"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	if want := []TagGroup{{Name: "Library", Tags: []string{"Library"}}}; !reflect.DeepEqual(doc.TagGroups, want) {
		t.Errorf("x-tagGroups = %+v, want %+v", doc.TagGroups, want)
	}
	for _, want := range []string{`"x-commerce-conditions"`, `"example": "Pride and Prejudice"`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s", want)
		}
	}
}
