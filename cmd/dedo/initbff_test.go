package main

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
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
	for _, mod := range otelModules {
		want[mod] = otelRelease
	}
	for mod, v := range want {
		if requires[mod] != v {
			t.Errorf("bff/go.mod requires %s %q, want %s", mod, requires[mod], v)
		}
	}
	if len(gomod.Replace) == 0 || gomod.Replace[0].Old.Path != "example.com/shop" || gomod.Replace[0].New.Path != "../" {
		t.Errorf("bff/go.mod replaces %v, want example.com/shop => ../ first", gomod.Replace)
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
		"      path: shop/web/v1\n      value: example.com/shop/bff/gen/go\n",
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
		"var buildSHA string", "$(git rev-parse HEAD | cut -c1-12)", "bfftools.CheckSHA(buildSHA)", "bfftools.Forward()", "bfftools.NewActivity(buildSHA, bfftools.ActivityOptions{Log: log})",
		"bfftools.Pin(buildSHA, prefix, activity.Middleware(api))", `"example.com/shop/bff/gen/go/shop/web/v1/webv1connect"`,
		"webv1connect.RegisterWebServiceHandler(server, newWebService(backend))",
		"setupTelemetry(ctx, log)", "telemetry.NewLogHandler(", "telemetry.NewClientInterceptor(telemetry.Config{})",
		"connect.NewServer(flowid.Interceptor(), observe)", `const serviceName = "bff"`)
	contains(t, "bff/web_service.go", read(t, dir, "bff/web_service.go"),
		`"example.com/shop/gen/go/shop/orders/v1/ordersv1connect"`,
		"orderService ordersv1connect.OrderServiceClient", "ordersv1connect.NewOrderServiceClient(backend)")

	if strings.Contains(read(t, dir, "go.mod"), "ignore") {
		t.Error("go.mod ignores a frontend that does not exist")
	}
	contains(t, "output", out,
		"frontend/: none; once the frontend lives there, add `ignore ./frontend` to go.mod",
		"buf generate --template bff/buf.gen.yaml", "go mod tidy",
		`go build -ldflags "-X main.buildSHA=$(git rev-parse HEAD | cut -c1-12)" .`)
	for _, name := range []string{"bff/main.go", "output"} {
		src := out
		if name != "output" {
			src = read(t, dir, name)
		}
		if strings.Contains(src, "--short") {
			t.Errorf("%s builds with git rev-parse --short, which gives more than 12 characters when 12 are ambiguous:\n%s", name, src)
		}
	}

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
// a buf.yaml entry its missing breaking ignore, and a name a warning. Its
// protos declare no service, so no server is written for one, and its
// package gets the BFF's Go prefix.
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
	for _, name := range []string{"bff/main.go", "bff/web_service.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was written for a starter service the BFF does not have", name)
		}
	}
	// dedo maintains only its replace of the MCP Go SDK there.
	want := "module example.com/shop/bff\n\n" + sdkForkComment + "\nreplace " + sdkModule + " " + sdkRelease + " => " + sdkFork + " " + sdkForkRelease + "\n"
	if got := read(t, dir, "bff/go.mod"); got != want {
		t.Errorf("bff/go.mod = %q, want %q", got, want)
	}
	contains(t, "output", out,
		"buf.yaml: WARNING bff/proto is named buf.build/acme/shop-web",
		"buf.yaml: bff/proto has no breaking check", "bff/go.mod: exists; left as it is",
		"bff/proto/ declares no service")
	contains(t, "bff/buf.gen.yaml", read(t, dir, "bff/buf.gen.yaml"),
		"      value: example.com/shop/gen/go\n",
		"      path: shop/ui/v1\n      value: example.com/shop/bff/gen/go\n")
	if strings.Contains(read(t, dir, "bff/buf.gen.yaml"), "shop/web") {
		t.Errorf("bff/buf.gen.yaml gives the starter's package a prefix:\n%s", read(t, dir, "bff/buf.gen.yaml"))
	}
	mods, err := bufModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Errorf("buf.yaml modules = %+v", mods)
	}
}

// connectStub stands in for the Connect code buf generate writes for a
// service: the declarations main.go and a handler use.
func connectStub(pkg, svc, source string) string {
	return "// Code generated by protoc-gen-connect-go. DO NOT EDIT.\n//\n// Source: " + source + "\n\n" +
		"package " + pkg + "\n\nimport \"connectrpc.com/connect/v2\"\n\n" +
		"type " + svc + "Handler interface{}\n\ntype Unimplemented" + svc + "Handler struct{}\n\n" +
		"func Register" + svc + "Handler(*connect.Server, " + svc + "Handler) {}\n"
}

// compilingRepo is a backend whose go.mod resolves from the module cache,
// with a stand-in for its generated OrderService client.
func compilingRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/shop\n\ngo 1.26.1\n\nrequire connectrpc.com/connect/v2 " + connectRelease + "\n",
		"gen/go/shop/orders/v1/ordersv1connect/orders.connect.go": "// Code generated by protoc-gen-connect-go. DO NOT EDIT.\n//\n// Source: shop/orders/v1/orders.proto\n\n" +
			"package ordersv1connect\n\nimport \"connectrpc.com/connect/v2\"\n\n" +
			"type OrderServiceClient interface{}\n\nfunc NewOrderServiceClient(*connect.Client) OrderServiceClient { return nil }\n",
	}
	for name, content := range extra {
		files[name] = content
	}
	return bffRepo(t, files)
}

// offlineGo points bff/go.mod at this working tree's dedo and keeps go to
// the module cache, which building dedo filled.
func offlineGo(t *testing.T, dir string) {
	t.Helper()
	dedoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "bff/go.mod", read(t, dir, "bff/go.mod")+"\nreplace github.com/raveesh-me/dedo => "+dedoRoot+"\n")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
}

// buildBFF builds and vets the BFF in dir/bff, with stand-ins for the code
// buf generate writes: buf generate needs the BSR.
func buildBFF(t *testing.T, dir string, generated map[string]string) {
	t.Helper()
	for name, src := range generated {
		writeFile(t, dir, name, src)
	}
	offlineGo(t, dir)
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = filepath.Join(dir, "bff")
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "GOPROXY=off") {
				t.Skipf("the module cache lacks a dependency: %s", out)
			}
			t.Fatalf("go %s in bff/: %v\n%s\nmain.go:\n%s", strings.Join(args, " "), err, out, read(t, dir, "bff/main.go"))
		}
	}
}

// The starter BFF dedo init -bff writes compiles against the code buf
// generate writes for it.
func TestInitBFFStarterCompiles(t *testing.T) {
	dir := compilingRepo(t, nil)
	initBFFOffline(t, dir)
	buildBFF(t, dir, map[string]string{
		"bff/gen/go/shop/web/v1/webv1connect/web.connect.go": connectStub("webv1connect", "WebService", "shop/web/v1/web.proto"),
	})
}

// When bff/proto has protos already, the server is written for the
// services they declare, never for a starter that is not there, and
// managed mode gives their packages the BFF's Go prefix. It compiles.
func TestInitBFFServesExistingProtos(t *testing.T) {
	dir := compilingRepo(t, map[string]string{
		"bff/proto/shop/ui/v1/ui.proto":         "syntax = \"proto3\";\npackage shop.ui.v1;\nservice UiService {}\nservice CartService {}\n",
		"bff/proto/shop/ui/v1/types.proto":      "syntax = \"proto3\";\npackage shop.ui.v1;\nmessage Money {}\n",
		"bff/proto/shop/forms/v1/forms.proto":   "syntax = \"proto3\";\npackage shop.forms.v1;\n// service CommentedService {\nservice FormService {}\n",
		"bff/proto/shop/labels/v1/labels.proto": "syntax = \"proto3\";\npackage shop.labels.v1;\nmessage Label {}\n",
	})
	out := initBFFOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "bff/proto/shop/web")); err == nil {
		t.Error("a starter service was written beside the BFF's protos")
	}
	if _, err := os.Stat(filepath.Join(dir, "bff/web_service.go")); err == nil {
		t.Error("bff/web_service.go was written for a starter service the BFF does not have")
	}
	main := read(t, dir, "bff/main.go")
	contains(t, "bff/main.go", main,
		`"example.com/shop/bff/gen/go/shop/ui/v1/uiv1connect"`, `"example.com/shop/bff/gen/go/shop/forms/v1/formsv1connect"`,
		"uiv1connect.RegisterUiServiceHandler(server, newUiService(backend))",
		"uiv1connect.RegisterCartServiceHandler(server, newCartService(backend))",
		"formsv1connect.RegisterFormServiceHandler(server, newFormService(backend))")
	if strings.Contains(main, "WebService") || strings.Contains(main, "CommentedService") {
		t.Errorf("bff/main.go registers a service the BFF does not declare:\n%s", main)
	}
	contains(t, "bff/ui_service.go", read(t, dir, "bff/ui_service.go"), "uiv1connect.UnimplementedUiServiceHandler", "orderService ordersv1connect.OrderServiceClient")
	contains(t, "bff/form_service.go", read(t, dir, "bff/form_service.go"), "formsv1connect.UnimplementedFormServiceHandler")
	gen := read(t, dir, "bff/buf.gen.yaml")
	contains(t, "bff/buf.gen.yaml", gen,
		"      path: shop/forms/v1\n      value: example.com/shop/bff/gen/go\n",
		"      path: shop/labels/v1\n      value: example.com/shop/bff/gen/go\n",
		"      path: shop/ui/v1\n      value: example.com/shop/bff/gen/go\n")
	if strings.Index(gen, "value: example.com/shop/gen/go") > strings.Index(gen, "path: shop/forms/v1") {
		t.Errorf("the backend's catch-all comes after the BFF's overrides, so it wins:\n%s", gen)
	}
	contains(t, "output", out, "bff/proto/: has protos already; no starter service", "UiService", "FormService")
	buildBFF(t, dir, map[string]string{
		"bff/gen/go/shop/ui/v1/uiv1connect/ui.connect.go":          connectStub("uiv1connect", "UiService", "shop/ui/v1/ui.proto") + "\ntype CartServiceHandler interface{}\n\ntype UnimplementedCartServiceHandler struct{}\n\nfunc RegisterCartServiceHandler(*connect.Server, CartServiceHandler) {}\n",
		"bff/gen/go/shop/forms/v1/formsv1connect/forms.connect.go": connectStub("formsv1connect", "FormService", "shop/forms/v1/forms.proto"),
	})
}

// A backend with a module web cannot take the starter's package; the
// recovery its error names works: once bff/proto has the BFF's protos,
// dedo init -bff sets the BFF up around them.
func TestInitBFFWebModuleRecovery(t *testing.T) {
	dir := bffRepo(t, map[string]string{
		"proto/shop/web/v1/web.proto": "syntax = \"proto3\";\npackage shop.web.v1;\nservice WebService {}\n",
	})
	var first bytes.Buffer
	err := run([]string{"init", "-bff", "-offline", "-dir", dir}, &first)
	if err == nil || !strings.Contains(err.Error(), "write the BFF's proto into bff/proto yourself, then run dedo init -bff again") {
		t.Fatalf("err = %v", err)
	}
	for _, withGoMod := range []bool{false, true} {
		if withGoMod {
			writeFile(t, dir, "bff/go.mod", "module example.com/shop/bff\n")
		}
		writeFile(t, dir, "bff/proto/shop/front/v1/front.proto", "syntax = \"proto3\";\npackage shop.front.v1;\nservice FrontService {}\n")
		initBFFOffline(t, dir)
		contains(t, "bff/main.go", read(t, dir, "bff/main.go"), "frontv1connect.RegisterFrontServiceHandler")
		if _, err := os.Stat(filepath.Join(dir, "bff/proto/shop/web")); err == nil {
			t.Error("a starter service was written into the backend's web package")
		}
	}
}

// The root buf.gen.yaml must keep its generators off the BFF's protos:
// with no inputs, buf generate runs them over the whole workspace, the
// BFF's module included.
func TestInitBFFChecksRootInputs(t *testing.T) {
	gen := "version: v2\nplugins:\n  - local: protoc-gen-go\n    out: gen/go\n"
	for _, c := range []struct {
		name, inputs string
		warn         bool
	}{
		{"no inputs", "", true},
		{"the proto root", "inputs:\n  - directory: proto\n", false},
		{"the BFF's module too", "inputs:\n  - directory: proto\n  - directory: bff/proto\n", true},
		{"the repository", "inputs:\n  - directory: .\n", true},
		// dedo modules reads the inputs the same way: exclude_paths
		// filters by a path inside the BFF's module, and buf ignores one
		// that holds the module.
		{"the repository, the BFF's protos excluded", "inputs:\n  - directory: .\n    exclude_paths: [bff/proto/shop]\n", false},
		{"the repository, the BFF's directory excluded", "inputs:\n  - directory: .\n    exclude_paths: [bff]\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := bffRepo(t, map[string]string{"buf.gen.yaml": gen + c.inputs})
			out := initBFFOffline(t, dir)
			if got := strings.Contains(out, "buf.gen.yaml: WARNING"); got != c.warn {
				t.Errorf("warned %v, want %v:\n%s", got, c.warn, out)
			}
			if c.warn {
				contains(t, "output", out, "inputs:\n      - directory: proto")
			}
		})
	}
	// Without a root buf.gen.yaml there is nothing to check.
	dir := bffRepo(t, nil)
	if out := initBFFOffline(t, dir); strings.Contains(out, "buf.gen.yaml: WARNING") {
		t.Errorf("warned without a buf.gen.yaml:\n%s", out)
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
	dir := compilingRepo(t, nil)
	initBFFOffline(t, dir)
	writeFile(t, dir, "bff/gen/go/shop/web/v1/webv1connect/web.connect.go", connectStub("webv1connect", "WebService", "shop/web/v1/web.proto"))
	offlineGo(t, dir)
	var out bytes.Buffer
	if err := run([]string{"modules", "-dir", dir}, &out); err != nil {
		if strings.Contains(out.String()+err.Error(), "GOPROXY=off") {
			t.Skipf("the module cache lacks a dependency: %v", err)
		}
		t.Fatalf("dedo modules: %v\n%s", err, out.String())
	}
	contains(t, "dedo modules", out.String(), "1 module in shop", "bff: bff (frontend; not a backend module)")
}
