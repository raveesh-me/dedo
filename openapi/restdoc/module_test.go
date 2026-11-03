package restdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		{Name: "Product", Description: "ProductService reads the products on sale.", Parent: "acme.catalog.v1"},
		{Name: "acme.orders.v1", Summary: "Orders", Description: "Orders is what buyers commit to buy, and what they send back.", Kind: "nav"},
		{Name: "Orders", Description: "OrderService reads orders. Its title is the package's title too: the\npackage's tag is named after the package, so the two cannot clash.", Parent: "acme.orders.v1"},
		{Name: "Return", Description: "ReturnService reads returns.", Parent: "acme.orders.v1"},
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
