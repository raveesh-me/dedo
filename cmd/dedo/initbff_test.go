package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

// orderProto is a backend service with the declarations dedo init -bff
// takes the BFF's names from.
const orderProto = `syntax = "proto3";

package shop.orders.v1;

import "dedo/v1/options.proto";
import "google/api/annotations.proto";

// OrderService takes orders.
service OrderService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "orders-team"
  };

  // GetOrder returns an order.
  rpc GetOrder(GetOrderRequest) returns (GetOrderResponse) {
    option (google.api.http) = {get: "/acme/shop/v1/{name=orders/*}"};
    option (dedo.v1.method) = {scopes: "shop.order.read"};
  }
}

message Order {
  option (google.api.resource) = {
    type: "shop.acme.dev/Order"
    pattern: "orders/{order}"
  };
  string name = 1;
}
`

// bffRepo is a backend dedo init set up, with buf.yaml's comments to keep.
func bffRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/shop\n\ngo 1.26.1\n\nrequire github.com/raveesh-me/dedo v0.30.0\n",
		"buf.yaml": "version: v2\n# The backend's API.\nmodules:\n  - path: proto\n" +
			"deps:\n  - buf.build/r-a/dedo:v0.30.0\n",
		"proto/shop/orders/v1/orders.proto": orderProto,
	}
	for name, content := range extra {
		files[name] = content
	}
	return repo(t, files)
}

func initBFFOffline(t *testing.T, dir string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run([]string{"init", "-bff", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatalf("dedo init -bff: %v\n%s", err, out.String())
	}
	return out.String()
}

// contains fails for each of wants that src lacks.
func contains(t *testing.T, name, src string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(src, want) {
			t.Errorf("%s lacks %q:\n%s", name, want, src)
		}
	}
}

// A backend gets a BFF named after it: its own Go module that reaches the
// backend's generated clients, an unnamed buf module with no breaking
// check, a starter service in the backend's context, its generators and
// its server.
func TestInitBFF(t *testing.T) {
	dir := bffRepo(t, nil)
	out := initBFFOffline(t, dir)

	gomod, err := modfile.Parse("go.mod", []byte(read(t, dir, "bff/go.mod")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if gomod.Module.Mod.Path != "example.com/shop/bff" || gomod.Go.Version != "1.26.1" {
		t.Errorf("bff/go.mod: module %s, go %s", gomod.Module.Mod.Path, gomod.Go.Version)
	}
	requires := map[string]string{}
	for _, r := range gomod.Require {
		requires[r.Mod.Path] = r.Mod.Version
	}
	// This release, newer than the backend's: MVS would choose it anyway.
	want := map[string]string{"example.com/shop": "v0.0.0", "github.com/raveesh-me/dedo": version, "connectrpc.com/connect/v2": connectRelease}
	for mod, v := range want {
		if requires[mod] != v {
			t.Errorf("bff/go.mod requires %s %q, want %s", mod, requires[mod], v)
		}
	}
	if len(gomod.Replace) != 1 || gomod.Replace[0].Old.Path != "example.com/shop" || gomod.Replace[0].New.Path != "../" {
		t.Errorf("bff/go.mod replaces %v, want example.com/shop => ../", gomod.Replace)
	}

	mods, err := bufModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 || mods[1] != (bufModule{Path: "bff/proto"}) {
		t.Errorf("buf.yaml modules = %+v, want proto, then bff/proto unnamed", mods)
	}
	entry := lookup(yamlAt(t, dir, "buf.yaml"), "modules").Content[1]
	if got := scalars(lookup(lookup(entry, "breaking"), "ignore")); len(got) != 1 || got[0] != "bff/proto" {
		t.Errorf("bff/proto breaking.ignore = %v", got)
	}
	contains(t, "buf.yaml", read(t, dir, "buf.yaml"), "# The backend's API.", "buf push\n  # --exclude-unnamed never publishes it")

	proto := read(t, dir, "bff/proto/shop/web/v1/web.proto")
	contains(t, "web.proto", proto,
		"package shop.web.v1;", "service WebService {", "audience: AUDIENCE_COMPONENT_INTERNAL",
		`owner_team: "orders-team"`, `scopes: "shop.order.read"`, `type: "shop.acme.dev/Page"`,
		`get: "/acme/shop/web/v1/{name=pages/*}"`)
	if got := serviceAudiences(proto); len(got) != 1 || got[0] != [2]string{"WebService", bffAudience} {
		t.Errorf("dedo modules reads the starter's audiences as %v", got)
	}

	gen := read(t, dir, "bff/buf.gen.yaml")
	contains(t, "bff/buf.gen.yaml", gen,
		"      value: example.com/shop/gen/go\n",
		"      path: shop/web\n      value: example.com/shop/bff/gen/go\n",
		"out: bff/gen/go", "- directory: bff/proto", "# - local:")
	for _, unwanted := range []string{"protoc-gen-dedo-openapi", "protoc-gen-dedo-mcp", "module: buf.build/acme"} {
		if strings.Contains(gen, unwanted) {
			t.Errorf("bff/buf.gen.yaml has %s:\n%s", unwanted, gen)
		}
	}

	for _, name := range []string{"bff/main.go", "bff/web_service.go"} {
		src := read(t, dir, name)
		if formatted, err := format.Source([]byte(src)); err != nil || string(formatted) != src {
			t.Errorf("%s is not gofmt-formatted (%v):\n%s", name, err, src)
		}
	}
	contains(t, "bff/main.go", read(t, dir, "bff/main.go"),
		"var buildSHA string", "bff.CheckSHA(buildSHA)", "bff.Forward()", "bff.NewActivity(buildSHA)",
		"bff.Pin(buildSHA, prefix, activity.Middleware(api))", `"example.com/shop/bff/gen/go/shop/web/v1/webv1connect"`,
		"webv1connect.RegisterWebServiceHandler(server, newWebService(backend))")
	contains(t, "bff/web_service.go", read(t, dir, "bff/web_service.go"),
		`"example.com/shop/gen/go/shop/orders/v1/ordersv1connect"`,
		"orderService ordersv1connect.OrderServiceClient", "ordersv1connect.NewOrderServiceClient(backend)")

	if strings.Contains(read(t, dir, "go.mod"), "ignore") {
		t.Error("go.mod ignores a frontend that does not exist")
	}
	contains(t, "output", out,
		"frontend/: none; once the frontend lives there, add `ignore ./frontend` to go.mod",
		"buf generate --template bff/buf.gen.yaml", "go mod tidy")

	// The backend's own setup never takes the BFF for one of its modules.
	p, err := analyze(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.services) != 1 || p.services[0].Name != "OrderService" || p.protoRoot != "proto" {
		t.Errorf("analyze: services %v, root %q", p.services, p.protoRoot)
	}

	before := snapshot(t, dir)
	again := initBFFOffline(t, dir)
	if !sameFiles(before, snapshot(t, dir)) {
		t.Errorf("a second dedo init -bff changed files:\n%s", again)
	}
	contains(t, "second output", again, "buf.yaml: lists bff/proto already", "bff/proto/: has protos already; no starter service")
}

// When the backend's buf module is named, as a published one is, managed
// mode reaches the backend's Go code by the module's name, and the BFF's
// is the catch-all.
func TestInitBFFNamedBackend(t *testing.T) {
	dir := bffRepo(t, map[string]string{
		"buf.yaml": "version: v2\nmodules:\n  - path: proto\n    name: buf.build/acme/shop\n",
		"buf.gen.yaml": "version: v2\nmanaged:\n  enabled: true\n  override:\n" +
			"    - file_option: go_package_prefix\n      value: example.com/shop/internal/gen\n",
	})
	initBFFOffline(t, dir)
	contains(t, "bff/buf.gen.yaml", read(t, dir, "bff/buf.gen.yaml"),
		"    - file_option: go_package_prefix\n      value: example.com/shop/bff/gen/go\n",
		"      module: buf.build/acme/shop\n      value: example.com/shop/internal/gen\n")
	contains(t, "bff/web_service.go", read(t, dir, "bff/web_service.go"), `"example.com/shop/internal/gen/shop/orders/v1/ordersv1connect"`)
}

// A frontend in frontend/ is left out of the backend's Go module, once.
func TestInitBFFIgnoresFrontend(t *testing.T) {
	dir := bffRepo(t, map[string]string{"frontend/package.json": "{}\n"})
	out := initBFFOffline(t, dir)
	contains(t, "output", out, "go.mod: added ignore ./frontend")
	initBFFOffline(t, dir)
	f, err := modfile.Parse("go.mod", []byte(read(t, dir, "go.mod")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Ignore) != 1 || f.Ignore[0].Path != "./frontend" {
		t.Errorf("go.mod ignores %v, want ./frontend once", f.Ignore)
	}
}

// What the repository has already is kept: its BFF protos get no starter,
// a buf.yaml entry its missing breaking ignore, and a name a warning.
func TestInitBFFKeepsWhatIsThere(t *testing.T) {
	dir := bffRepo(t, map[string]string{
		"buf.yaml":                      "version: v2\nmodules:\n  - path: proto\n  - path: bff/proto\n    name: buf.build/acme/shop-web\n",
		"bff/proto/shop/ui/v1/ui.proto": "syntax = \"proto3\";\npackage shop.ui.v1;\n",
		"bff/go.mod":                    "module example.com/shop/bff\n",
	})
	out := initBFFOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "bff/proto/shop/web")); err == nil {
		t.Error("a starter service was written beside the BFF's protos")
	}
	if got := read(t, dir, "bff/go.mod"); got != "module example.com/shop/bff\n" {
		t.Errorf("bff/go.mod was changed: %q", got)
	}
	contains(t, "output", out,
		"buf.yaml: WARNING bff/proto is named buf.build/acme/shop-web",
		"buf.yaml: bff/proto has no breaking check", "bff/go.mod: exists; left as it is")
	mods, err := bufModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Errorf("buf.yaml modules = %+v", mods)
	}
}

func TestInitBFFNeedsABackend(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no go.mod", map[string]string{"proto/a/b/v1/b.proto": "syntax = \"proto3\";\npackage a.b.v1;\n"}, "has no go.mod"},
		{"no buf.yaml", map[string]string{"go.mod": "module example.com/shop\n"}, "run dedo init first"},
		{"a web module", map[string]string{
			"go.mod":                      "module example.com/shop\n",
			"buf.yaml":                    "version: v2\nmodules:\n  - path: proto\n",
			"proto/shop/web/v1/web.proto": "syntax = \"proto3\";\npackage shop.web.v1;\n",
		}, "the backend has a module web in shop already"},
		{"buf.yaml v1", map[string]string{
			"go.mod":   "module example.com/shop\n",
			"buf.yaml": "version: v1\n",
		}, "not version v2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := repo(t, tt.files)
			var out bytes.Buffer
			err := run([]string{"init", "-bff", "-offline", "-dir", dir}, &out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// dedo modules accepts the BFF dedo init -bff writes, built against this
// working tree's runtime. buf generate needs the BSR, so the generated
// packages are stand-ins with the declarations the BFF uses; go list
// resolves the rest from the module cache, which building dedo filled.
func TestInitBFFPassesModules(t *testing.T) {
	dedoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := bffRepo(t, map[string]string{
		"go.mod": "module example.com/shop\n\ngo 1.26.1\n\nrequire connectrpc.com/connect/v2 " + connectRelease + "\n",
		"gen/go/shop/orders/v1/ordersv1connect/orders.connect.go": "// Code generated by protoc-gen-connect-go. DO NOT EDIT.\n//\n// Source: shop/orders/v1/orders.proto\n\n" +
			"package ordersv1connect\n\nimport \"connectrpc.com/connect/v2\"\n\n" +
			"type OrderServiceClient interface{}\n\nfunc NewOrderServiceClient(*connect.Client) OrderServiceClient { return nil }\n",
	})
	initBFFOffline(t, dir)
	writeFile(t, dir, "bff/gen/go/shop/web/v1/webv1connect/web.connect.go", "// Code generated by protoc-gen-connect-go. DO NOT EDIT.\n//\n// Source: shop/web/v1/web.proto\n\n"+
		"package webv1connect\n\nimport \"connectrpc.com/connect/v2\"\n\n"+
		"type WebServiceHandler interface{}\n\ntype UnimplementedWebServiceHandler struct{}\n\n"+
		"func RegisterWebServiceHandler(*connect.Server, WebServiceHandler) {}\n")
	gomod := read(t, dir, "bff/go.mod")
	writeFile(t, dir, "bff/go.mod", gomod+"\nreplace github.com/raveesh-me/dedo => "+dedoRoot+"\n")

	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	var out bytes.Buffer
	if err := run([]string{"modules", "-dir", dir}, &out); err != nil {
		if strings.Contains(out.String()+err.Error(), "GOPROXY=off") {
			t.Skipf("the module cache lacks a dependency: %v", err)
		}
		t.Fatalf("dedo modules: %v\n%s", err, out.String())
	}
	contains(t, "dedo modules", out.String(), "1 module in shop", "bff: bff (frontend; not a backend module)")
}
