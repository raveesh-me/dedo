package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// overlay copies testdata/modules/base, then each layer's files over it,
// into a new directory: a service repository with one change.
func overlay(t *testing.T, layers ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, src := range append([]string{"base"}, layers...) {
		root := filepath.Join("testdata", "modules", src)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			writeFile(t, dir, rel, string(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// dedo modules on each repository: the findings it prints, as lines, and
// the graph when there are none.
func TestModules(t *testing.T) {
	// The repositories are Go modules of their own, outside dedo's workspace.
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	for _, tt := range []struct {
		name string
		// over is a layer the case's files go over, such as bff, a clean
		// backend for frontend.
		over string
		// want are lines of the output, in order; findings fail the run.
		want     []string
		findings int
	}{
		{name: "base", want: []string{
			"dedo modules: 2 modules in shop, no cycles; each depends on:",
			"  billing  (nothing)",
			"  orders   -> billing",
		}},
		{name: "import-cycle", findings: 1, want: []string{
			"modules billing, orders depend on each other: make them one module, or move what they share to a third",
			"    billing -> orders: internal/billing -> internal/orders",
			"    orders -> billing: internal/orders -> gen/go/shop/billing/v1/billingv1connect",
		}},
		{name: "client-cycle", findings: 1, want: []string{
			"modules billing, orders depend on each other: make them one module, or move what they share to a third",
			"    billing -> orders: internal/billing -> gen/go/shop/orders/v1/ordersv1connect",
			"    orders -> billing: internal/orders -> gen/go/shop/billing/v1/billingv1connect",
		}},
		{name: "shared-imports-module", findings: 2, want: []string{
			"gen/go/shop/common is shared but imports module orders (gen/go/shop/orders/v1): shared code imports no module",
			"internal/shared/report is shared but imports module billing (internal/billing): shared code imports no module",
		}},
		{name: "misplaced-module", findings: 4, want: []string{
			`a proto package names module "shared", which is reserved for shared code: rename the package`,
			"app/orders: module orders lives in internal/orders, nowhere else",
			"internal/tools: internal/ holds only modules (proto packages <context>.<m>.v<n>) and shared",
			"pkg/billing: module billing lives in internal/billing, nowhere else",
		}},
		{name: "sql-violation", findings: 4, want: []string{
			"internal/billing/migrations/0002_fk.sql: module billing's SQL names module orders's schema: read it through orders.Port",
			"internal/orders/queries/report.sql: module orders's SQL names module billing's schema: read it through billing.Port",
			"internal/shared/db/report.sql: shared SQL names module billing's schema",
			"internal/orders/store.go:3: module orders's SQL names module billing's schema: read it through billing.Port",
		}},
		{name: "v1-and-v2", want: []string{
			"dedo modules: 2 modules in shop, no cycles; each depends on:",
			"  billing  (nothing)",
			"  orders   -> billing",
		}},
		{name: "v2-cycle", findings: 1, want: []string{
			"modules billing, orders depend on each other: make them one module, or move what they share to a third",
			"    billing -> orders: internal/billing -> internal/orders",
			"    orders -> billing: internal/orders -> gen/go/shop/billing/v2/billingv2connect",
		}},
		// A backend for frontend, a Go module of its own in bff/, is not a
		// backend module: its protos add none, and it reads the backend
		// through the generated client.
		{name: "bff", want: []string{
			"dedo modules: 2 modules in shop, no cycles; each depends on:",
			"  billing  (nothing)",
			"  orders   -> billing",
			"bff: bff (frontend; not a backend module)",
		}},
		{name: "bff-named", over: "bff", findings: 1, want: []string{
			"bff/proto: buf.yaml names the module buf.build/acme/shop-web, which serves the frontend of the Go module in bff: remove the name, so buf push --exclude-unnamed never publishes it",
		}},
		{name: "bff-published-internal", over: "bff", findings: 1, want: []string{
			"proto/shop/orders/v1/orders.proto: service OrderService is AUDIENCE_COMPONENT_INTERNAL in buf.build/acme/shop, a published module: only a backend for frontend's services are; declare the audience of its callers",
		}},
		// A BFF's service is its frontend's only, or external where the
		// house rules want every service external; never another
		// internal audience, and never none.
		{name: "bff-audience", over: "bff", findings: 3, want: []string{
			"bff/proto/shop/web/v1/web.proto: service WebService is AUDIENCE_COMPANY_INTERNAL, but serves the frontend of the Go module in bff: declare audience: AUDIENCE_COMPONENT_INTERNAL, AUDIENCE_EXTERNAL_PARTNER or AUDIENCE_EXTERNAL_PUBLIC",
			"bff/proto/shop/web/v1/web.proto: service CartService declares no audience, but serves the frontend of the Go module in bff: declare audience: AUDIENCE_COMPONENT_INTERNAL, AUDIENCE_EXTERNAL_PARTNER or AUDIENCE_EXTERNAL_PUBLIC",
			"bff/proto/shop/web/v1/web.proto: service ReportService is AUDIENCE_BUSINESS_UNIT_INTERNAL, but serves the frontend of the Go module in bff: declare audience: AUDIENCE_COMPONENT_INTERNAL, AUDIENCE_EXTERNAL_PARTNER or AUDIENCE_EXTERNAL_PUBLIC",
		}},
		{name: "bff-external", over: "bff", want: []string{
			"dedo modules: 2 modules in shop, no cycles; each depends on:",
			"  billing  (nothing)",
			"  orders   -> billing",
			"bff: bff (frontend; not a backend module)",
		}},
		// A driver counts however the BFF reaches it; database/sql/driver,
		// which value types import, is not one.
		{name: "bff-database", over: "bff", findings: 2, want: []string{
			"bff/report: depends on the database driver database/sql through example.com/store/cache -> example.com/store/db: a backend for frontend owns no data; read it through the backend's Connect API",
			"bff/web: depends on the database driver database/sql: a backend for frontend owns no data; read it through the backend's Connect API",
		}},
		{name: "bff-backend-import", over: "bff", findings: 2, want: []string{
			"bff/web: imports docs, the backend's own code: read the backend only through its Connect API, with the clients generated from its protos",
			"bff/web: imports internal/orders, the backend's own code: read the backend only through its Connect API, with the clients generated from its protos",
		}},
		// The backend's own code counts however the BFF reaches it, such
		// as through a module nested under the backend's path, which Go
		// lets import its internal/.
		{name: "bff-backend-indirect", over: "bff", findings: 1, want: []string{
			"bff/web: depends on internal/orders, the backend's own code, through example.com/shop/lib/rates: read the backend only through its Connect API, with the clients generated from its protos",
		}},
		{name: "bff-imported", over: "bff", findings: 1, want: []string{
			"internal/billing: imports example.com/shop/bff/web, the backend for frontend in bff: the backend never imports its frontend's BFF; move what they share into the backend",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			layers := []string{tt.name}
			if tt.over != "" {
				layers = []string{tt.over, tt.name}
			}
			dir := overlay(t, layers...)
			var out bytes.Buffer
			err := run([]string{"modules", "-dir", dir}, &out)
			switch {
			case tt.findings == 0 && err != nil:
				t.Fatalf("dedo modules: %v\n%s", err, out.String())
			case tt.findings > 0 && (err == nil || !strings.Contains(err.Error(), strconv.Itoa(tt.findings)+" finding(s)")):
				t.Fatalf("err = %v, want %d finding(s)\n%s", err, tt.findings, out.String())
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if !slices.Equal(lines, tt.want) {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// A repository without versioned proto packages has no modules to check.
func TestModulesWithoutModules(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/notes\n", "proto/notes/type.proto": "syntax = \"proto3\";\npackage notes;\n"})
	var out bytes.Buffer
	if err := run([]string{"modules", "-dir", dir}, &out); err != nil || !strings.Contains(out.String(), "no modules") {
		t.Errorf("err = %v, output %q", err, out.String())
	}
	if err := run([]string{"modules", "-dir", t.TempDir()}, &out); err == nil || !strings.Contains(err.Error(), "Go service repository") {
		t.Errorf("without go.mod: err = %v", err)
	}
}

// A backend for frontend is checked whatever the backend's state, such as
// in a new repository with no versioned proto packages yet.
func TestModulesBFFWithoutModules(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	web := func(audience string) string {
		return "syntax = \"proto3\";\npackage notes.web.v1;\nservice WebService {\n  option (dedo.v1.service).audience = " + audience + ";\n}\n"
	}
	for _, tt := range []struct {
		name, audience, main string
		want                 []string
		findings             int
	}{
		{name: "clean", audience: bffAudience, main: "package main\n\nfunc main() {}\n", want: []string{
			"dedo modules: no modules; a module is a versioned proto package, <context>.<m>.v<n>",
			"bff: bff (frontend; not a backend module)",
		}},
		{name: "external", audience: "AUDIENCE_EXTERNAL_PUBLIC", main: "package main\n\nfunc main() {}\n", want: []string{
			"dedo modules: no modules; a module is a versioned proto package, <context>.<m>.v<n>",
			"bff: bff (frontend; not a backend module)",
		}},
		{name: "violations", audience: "AUDIENCE_COMPANY_INTERNAL", main: "package main\n\nimport _ \"database/sql\"\n\nfunc main() {}\n", findings: 2, want: []string{
			"bff/proto/notes/web/v1/web.proto: service WebService is AUDIENCE_COMPANY_INTERNAL, but serves the frontend of the Go module in bff: declare audience: AUDIENCE_COMPONENT_INTERNAL, AUDIENCE_EXTERNAL_PARTNER or AUDIENCE_EXTERNAL_PUBLIC",
			"bff: depends on the database driver database/sql: a backend for frontend owns no data; read it through the backend's Connect API",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := repo(t, map[string]string{
				"go.mod":                           "module example.com/notes\n\ngo 1.26\n",
				"buf.yaml":                         "version: v2\nmodules:\n  - path: bff/proto\n  - path: proto\n",
				"proto/notes/type.proto":           "syntax = \"proto3\";\npackage notes;\n",
				"bff/go.mod":                       "module example.com/notes/bff\n\ngo 1.26\n",
				"bff/main.go":                      tt.main,
				"bff/proto/notes/web/v1/web.proto": web(tt.audience),
			})
			var out bytes.Buffer
			err := run([]string{"modules", "-dir", dir}, &out)
			switch {
			case tt.findings == 0 && err != nil:
				t.Fatalf("dedo modules: %v\n%s", err, out.String())
			case tt.findings > 0 && (err == nil || !strings.Contains(err.Error(), strconv.Itoa(tt.findings)+" finding(s)")):
				t.Fatalf("err = %v, want %d finding(s)\n%s", err, tt.findings, out.String())
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if !slices.Equal(lines, tt.want) {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestProtoModule(t *testing.T) {
	for _, tt := range []struct {
		pkg, context, module, version string
		ok                            bool
	}{
		{"shop.orders.v1", "shop", "orders", "v1", true},
		{"acme.shop.orders.v2beta1", "acme.shop", "orders", "v2beta1", true},
		{"notes.v1", "", "notes", "v1", true},
		{"shop.type", "", "", "", false},
		{"shop.orders.version", "", "", "", false},
		{"v1", "", "", "", false},
	} {
		context, module, version, ok := protoModule(tt.pkg)
		if context != tt.context || module != tt.module || version != tt.version || ok != tt.ok {
			t.Errorf("protoModule(%s) = %q %q %q %v", tt.pkg, context, module, version, ok)
		}
	}
}

const shop = "example.com/shop"

var shopLayout = Layout{
	Module:  shop,
	Modules: []string{"billing", "orders"},
	Generated: map[string]string{
		shop + "/gen/go/shop/orders/v1":                   "orders",
		shop + "/gen/go/shop/orders/v1/ordersv1connect":   "orders",
		shop + "/gen/go/shop/billing/v2/billingv2connect": "billing",
		shop + "/gen/go/shop/type":                        "",
	},
	Mains: map[string]bool{shop + "/cmd/shopctl": true},
}

func TestClassify(t *testing.T) {
	for _, tt := range []struct {
		path   string
		kind   Kind
		module string
	}{
		{shop, KindRoot, ""},
		{shop + "/app", KindRoot, ""},
		{shop + "/app/apptest", KindRoot, ""},
		{shop + "/cmd/shopctl", KindRoot, ""},
		{shop + "/cmd/shopctl/flags", KindRoot, ""},
		{shop + "/internal/orders", KindModule, "orders"},
		{shop + "/internal/orders/store", KindModule, "orders"},
		{shop + "/gen/go/shop/orders/v1", KindModule, "orders"},
		{shop + "/gen/go/shop/orders/v1/ordersv1connect", KindModule, "orders"},
		// Every version of a module is the same module.
		{shop + "/gen/go/shop/billing/v2/billingv2connect", KindModule, "billing"},
		{shop + "/internal/shared/db", KindShared, ""},
		{shop + "/gen/go/shop/type", KindShared, ""},
		{shop + "/docs", KindOther, ""},
		{shop + "x/internal/orders", KindOther, ""},
		{"github.com/jackc/pgx/v5", KindOther, ""},
	} {
		kind, module := shopLayout.Classify(tt.path)
		if kind != tt.kind || module != tt.module {
			t.Errorf("Classify(%s) = %v %q, want %v %q", tt.path, kind, module, tt.kind, tt.module)
		}
	}
}

func TestLooksLikeSQL(t *testing.T) {
	for s, want := range map[string]bool{
		"SELECT count(*) FROM orders.orders": true,
		"orders: export failed":              false,
		"nexus.count-sales":                  false,
		"shop.orders.order.placed":           false,
	} {
		if got := LooksLikeSQL(s); got != want {
			t.Errorf("LooksLikeSQL(%q) = %v", s, got)
		}
	}
}
