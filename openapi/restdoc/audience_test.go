package restdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

const storefrontProto = "acme/storefront/v1/storefront.proto"

// The storefront fixture adds a package with a component-internal service
// and a business-unit-internal one to the shop.
var storefrontProtos = append(slices.Clone(shopProtos), storefrontProto)

// audienceDoc is the part of a document the audience tests read.
type audienceDoc struct {
	Info struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Tags       []Tag                      `json:"tags"`
	Paths      map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

func decode(t *testing.T, out string) audienceDoc {
	t.Helper()
	var doc audienceDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func (d audienceDoc) tagNames() []string {
	var out []string
	for _, t := range d.Tags {
		out = append(out, t.Name)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestParseAudience(t *testing.T) {
	for _, c := range []struct {
		value string
		want  dedov1.Audience
	}{
		{"COMPONENT_INTERNAL", dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL},
		{"AUDIENCE_COMPONENT_INTERNAL", dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL},
		{"BUSINESS_UNIT_INTERNAL", dedov1.Audience_AUDIENCE_BUSINESS_UNIT_INTERNAL},
		{"COMPANY_INTERNAL", dedov1.Audience_AUDIENCE_COMPANY_INTERNAL},
		{"AUDIENCE_EXTERNAL_PARTNER", dedov1.Audience_AUDIENCE_EXTERNAL_PARTNER},
		{"EXTERNAL_PUBLIC", dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC},
	} {
		got, err := parseAudience(c.value)
		if err != nil || got != c.want {
			t.Errorf("parseAudience(%q) = %v, %v; want %v", c.value, got, err, c.want)
		}
	}
}

// An unknown audience is an error that names it and lists the known ones.
// AUDIENCE_UNSPECIFIED is no audience, so it is unknown too.
func TestParseAudienceRejectsUnknown(t *testing.T) {
	for _, value := range []string{"", "PUBLIC", "company_internal", "UNSPECIFIED", "AUDIENCE_UNSPECIFIED", "AUDIENCE_"} {
		_, err := parseAudience(value)
		if err == nil {
			t.Errorf("parseAudience(%q): want an error", value)
			continue
		}
		for _, want := range []string{"min_audience", `"` + value + `"`, "COMPONENT_INTERNAL, BUSINESS_UNIT_INTERNAL, COMPANY_INTERNAL, EXTERNAL_PARTNER, EXTERNAL_PUBLIC"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("parseAudience(%q) error %q does not mention %s", value, err, want)
			}
		}
	}
}

func TestConfigSetsMinAudience(t *testing.T) {
	var cfg Config
	if err := cfg.Set("min_audience", "COMPANY_INTERNAL"); err != nil {
		t.Fatal(err)
	}
	if cfg.MinAudience != dedov1.Audience_AUDIENCE_COMPANY_INTERNAL {
		t.Errorf("MinAudience = %v, want AUDIENCE_COMPANY_INTERNAL", cfg.MinAudience)
	}
	if err := cfg.Set("min_audience", "EVERYONE"); err == nil {
		t.Error("min_audience=EVERYONE: want an error")
	}
}

// By default the document leaves out the component-internal service: its
// path, its tag and its name in the title. Its Sku message is not reached,
// so the shop's Sku keeps its short name.
func TestGenerateLeavesOutComponentInternalServices(t *testing.T) {
	got := generate(t, "title=Shop API,filename=storefront.openapi.json", storefrontProtos...)
	golden := filepath.Join("testdata", "storefront.openapi.json")
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
	parsed, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	for _, leak := range []string{"ProductPage", "productPages", "Product pages", "acme.storefront.v1.Sku", "acme.type.Sku"} {
		if strings.Contains(got, leak) {
			t.Errorf("output mentions %s, which only the component-internal service reaches", leak)
		}
	}
}

func TestGenerateMinAudience(t *testing.T) {
	shop := []string{"/shop/v1/orders/{ordersId}", "/shop/v1/products/{productsId}", "/shop/v1/returns/{returnsId}"}
	for _, c := range []struct {
		params  string
		paths   []string
		tags    []string
		schemas []string
	}{
		{
			params:  "",
			paths:   append(slices.Clone(shop), "/shop/v1/stock/{stockId}"),
			tags:    []string{"acme.catalog.v1", "Product", "acme.orders.v1", "Orders", "Return", "acme.storefront.v1", "Stock"},
			schemas: []string{"GetOrderResponse", "GetProductResponse", "GetReturnResponse", "GetStockResponse", "Sku", "Status"},
		},
		{
			params:  "min_audience=COMPONENT_INTERNAL",
			paths:   []string{"/shop/v1/orders/{ordersId}", "/shop/v1/productPages/{productPagesId}", "/shop/v1/products/{productsId}", "/shop/v1/returns/{returnsId}", "/shop/v1/stock/{stockId}"},
			tags:    []string{"acme.catalog.v1", "Product", "acme.orders.v1", "Orders", "Return", "acme.storefront.v1", "Product pages", "Stock"},
			schemas: []string{"GetOrderResponse", "GetProductPageResponse", "GetProductResponse", "GetReturnResponse", "GetStockResponse", "Status", "acme.storefront.v1.Sku", "acme.type.Sku"},
		},
		{
			// A service without an audience, as catalog's and orders' are,
			// is kept whatever the minimum: lint rejects it instead.
			params:  "min_audience=AUDIENCE_COMPANY_INTERNAL",
			paths:   shop,
			tags:    []string{"acme.catalog.v1", "Product", "acme.orders.v1", "Orders", "Return"},
			schemas: []string{"GetOrderResponse", "GetProductResponse", "GetReturnResponse", "Sku", "Status"},
		},
	} {
		t.Run(c.params, func(t *testing.T) {
			doc := decode(t, generate(t, c.params, storefrontProtos...))
			if got := sortedKeys(doc.Paths); !slices.Equal(got, c.paths) {
				t.Errorf("paths = %q, want %q", got, c.paths)
			}
			if got := doc.tagNames(); !slices.Equal(got, c.tags) {
				t.Errorf("tags = %q, want %q", got, c.tags)
			}
			if got := sortedKeys(doc.Components.Schemas); !slices.Equal(got, c.schemas) {
				t.Errorf("schemas = %q, want %q", got, c.schemas)
			}
		})
	}
}

// The title and version come from the documented services only.
func TestGenerateTitleNamesDocumentedServicesOnly(t *testing.T) {
	for _, c := range []struct {
		params, title string
	}{
		{"", "ProductPageService"},
		{"min_audience=COMPANY_INTERNAL", "StockService"},
	} {
		doc := decode(t, generate(t, c.params, storefrontProto, "acme/catalog/v1/catalog.proto"))
		if strings.Contains(doc.Info.Title, c.title) {
			t.Errorf("%s: title %q names %s, which is left out", c.params, doc.Info.Title, c.title)
		}
		if !strings.Contains(doc.Info.Title, "ProductService") {
			t.Errorf("%s: title %q does not name ProductService", c.params, doc.Info.Title)
		}
	}
}

// A run that leaves out every service still writes a valid document, with
// no operations and no tags, so whatever embeds or serves it keeps working,
// and warns.
func TestGenerateLeavesOutEveryService(t *testing.T) {
	var warnings []string
	cfg := Config{
		MinAudience: dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC,
		Warn:        func(msg string) { warnings = append(warnings, msg) },
	}
	got := generateWith(t, cfg, storefrontProto)
	doc := decode(t, got)
	if len(doc.Paths) != 0 || len(doc.Tags) != 0 {
		t.Errorf("paths %q, tags %q; want none", sortedKeys(doc.Paths), doc.tagNames())
	}
	if doc.Info.Title != "API" || doc.Info.Version != "v1" {
		t.Errorf("info = %+v, want title API, version v1", doc.Info)
	}
	if got := sortedKeys(doc.Components.Schemas); !slices.Equal(got, []string{"Status"}) {
		t.Errorf("schemas = %q, want only Status", got)
	}
	parsed, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "every service (2)") || !strings.Contains(warnings[0], "min_audience") {
		t.Errorf("warnings = %q, want one saying every service (2) is left out by min_audience", warnings)
	}
}

// A run without services, such as one over value objects only, has none to
// leave out and does not warn.
func TestGenerateWithoutServicesDoesNotWarn(t *testing.T) {
	var warnings []string
	generateWith(t, Config{Warn: func(msg string) { warnings = append(warnings, msg) }}, "acme/type/type.proto")
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
}
