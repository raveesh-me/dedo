package main

import (
	"bytes"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const counterProto = `syntax = "proto3";

package acme.time_counter.v1;

import "dedo/v1/options.proto";

// CounterService counts time.
service CounterService {
  rpc GetCounter(GetCounterRequest) returns (GetCounterResponse) {
    option (dedo.v1.method) = {scopes: "time_counter.counter.read"};
  }
  rpc AddTime(AddTimeRequest) returns (AddTimeResponse) {
    option (dedo.v1.method) = {scopes: "time_counter.counter.write"};
  }
}
`

// repo writes files into a new directory.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func initOffline(t *testing.T, dir string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run([]string{"init", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatalf("dedo init: %v\n%s", err, out.String())
	}
	return out.String()
}

// mainGo returns the main.go dedo init wrote, or else the one it printed.
func mainGo(t *testing.T, dir, out string) string {
	t.Helper()
	if i := strings.Index(out, "// ---- main.go"); i >= 0 {
		return out[i+strings.Index(out[i:], "\n")+1:]
	}
	return read(t, dir, "main.go")
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func yamlAt(t *testing.T, dir, name string) *yaml.Node {
	t.Helper()
	doc, ok, err := loadYAML(filepath.Join(dir, name))
	if err != nil || !ok {
		t.Fatalf("%s: %v", name, err)
	}
	return root(doc)
}

// snapshot reads every file under dir.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		files[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A fresh repository gets every file, filled in with its names.
func TestInitFreshRepository(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
	})
	out := initOffline(t, dir)

	buf := yamlAt(t, dir, "buf.yaml")
	if got := scalars(lookup(buf, "deps")); !slices.Contains(got, optionsModule+":"+version) {
		t.Errorf("buf.yaml deps = %v", got)
	}
	if got := lookup(lookup(buf, "modules").Content[0], "path").Value; got != "proto" {
		t.Errorf("module path = %q, want proto", got)
	}
	if !strings.Contains(read(t, dir, "buf.yaml"), "plugin: "+lintPlugin) {
		t.Error("buf.yaml lacks the lint plugin")
	}
	gen := read(t, dir, "buf.gen.yaml")
	for _, want := range []string{"github.com/acme/time-counter/gen/go", "title=Time Counter API", "directory: proto", "protoc-gen-dedo-mcp"} {
		if !strings.Contains(gen, want) {
			t.Errorf("buf.gen.yaml lacks %q", want)
		}
	}
	if got := read(t, dir, "Makefile"); got != "include Makefile.api\n" {
		t.Errorf("Makefile = %q", got)
	}
	if !strings.Contains(read(t, dir, "Makefile.api"), "go tool dedo tools") {
		t.Error("Makefile.api does not run dedo tools")
	}
	if !strings.Contains(read(t, dir, ".gitignore"), ".tools/") {
		t.Error(".gitignore lacks .tools/")
	}
	if !strings.HasPrefix(read(t, dir, "AGENTS.md"), guidanceMarker) {
		t.Error("AGENTS.md lacks the guidance")
	}
	if !strings.Contains(read(t, dir, "docs/index.html"), "<title>Time Counter API</title>") {
		t.Error("docs/index.html has no title")
	}
	test := read(t, dir, "policy_table_test.go")
	if !strings.Contains(test, "time_counterv1connect.CounterServiceName") {
		t.Errorf("policy test:\n%s", test)
	}

	for _, want := range []string{
		"go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@" + version,
		"buf dep update",
		"go get -tool " + cliPkg + "@" + version,
		"run dedo tools",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(out, lintPluginPkg) {
		t.Errorf("the lint plugin is pinned in go.mod:\n%s", out)
	}
	main := mainGo(t, dir, out)
	formatted, err := format.Source([]byte(main))
	if err != nil {
		t.Fatalf("main.go does not parse: %v\n%s", err, main)
	}
	if string(formatted) != main {
		t.Errorf("main.go is not gofmt-formatted:\n%s", main)
	}
	for _, want := range []string{
		`time_counterv1 "github.com/acme/time-counter/gen/go/acme/time_counter/v1"`,
		`mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), time_counterv1.CounterServiceMCPTools()...)`,
		`"reader": {"time_counter.counter.read"}`,
		`"writer": {"time_counter.counter.read", "time_counter.counter.write"}`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s", want)
		}
	}

	// A second run changes nothing.
	before := snapshot(t, dir)
	initOffline(t, dir)
	if after := snapshot(t, dir); !maps(before, after) {
		t.Error("a second dedo init changed files")
	}
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// An existing setup is extended, not replaced: other deps, plugins and
// generators stay, and plugins from earlier dedo releases are replaced.
func TestInitExistingRepository(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":   "module example.com/shop\n",
		"Makefile": "build:\n\tgo build ./...",
		"buf.yaml": `version: v2
modules:
  - path: api
deps:
  - buf.build/acme/common # our shared types
lint:
  use:
    - DEFAULT
plugins:
  - plugin: .tools/buf-plugin-aip
  - plugin: .tools/buf-plugin-dedo
  - plugin: .tools/buf-plugin-house
`,
		"buf.gen.yaml": `version: v2
plugins:
  - local: protoc-gen-go
    out: gen/go
  - remote: buf.build/bufbuild/es
    out: web/gen
`,
		"CLAUDE.md":                     "# Shop\n\nOur rules.",
		"AGENTS.md":                     "notes\n\n" + guidanceMarker + "\n...\n",
		"api/shop/cart/v1/cart.proto":   "syntax = \"proto3\";\npackage shop.cart.v1;\nservice CartService {}\n",
		"api/shop/order/v1/order.proto": "syntax = \"proto3\";\npackage shop.order.v1;\nservice OrderService {}\n",
	})
	out := initOffline(t, dir)

	buf := yamlAt(t, dir, "buf.yaml")
	if got := scalars(lookup(buf, "deps")); !slices.Equal(got, []string{"buf.build/acme/common", googleapis, protovalidate, optionsModule + ":" + version}) {
		t.Errorf("deps = %v", got)
	}
	if got := scalars(lookup(lookup(buf, "lint"), "use")); !slices.Equal(got, []string{"DEFAULT", "DEDO"}) {
		t.Errorf("lint.use = %v", got)
	}
	var plugins []string
	for _, item := range lookup(buf, "plugins").Content {
		plugins = append(plugins, lookup(item, "plugin").Value)
	}
	if !slices.Equal(plugins, []string{".tools/buf-plugin-house", lintPlugin}) {
		t.Errorf("plugins = %v", plugins)
	}
	if !strings.Contains(read(t, dir, "buf.yaml"), "# our shared types") {
		t.Error("buf.yaml lost a comment")
	}

	gen := read(t, dir, "buf.gen.yaml")
	if strings.Contains(gen, "managed:") {
		t.Errorf("managed mode was turned on:\n%s", gen)
	}
	for _, want := range []string{"buf.build/bufbuild/es", "protoc-gen-connect-go", "protoc-gen-dedo-mcp", "protoc-gen-dedo-openapi"} {
		if !strings.Contains(gen, want) {
			t.Errorf("buf.gen.yaml lacks %q:\n%s", want, gen)
		}
	}
	if strings.Count(gen, `"protoc-gen-go"`)+strings.Count(gen, "local: protoc-gen-go\n") != 1 {
		t.Errorf("protoc-gen-go is listed more than once:\n%s", gen)
	}

	if got := read(t, dir, "Makefile"); got != "build:\n\tgo build ./...\ninclude Makefile.api\n" {
		t.Errorf("Makefile = %q", got)
	}
	if got := read(t, dir, "CLAUDE.md"); !strings.HasPrefix(got, "# Shop\n\nOur rules.\n\n"+guidanceMarker) {
		t.Errorf("CLAUDE.md = %.80q", got)
	}
	if got := read(t, dir, "AGENTS.md"); strings.Count(got, guidanceMarker) != 1 {
		t.Error("AGENTS.md got the guidance twice")
	}
	if !strings.Contains(out, "1 more services") {
		t.Errorf("output does not mention the second service:\n%s", out)
	}
}

func TestInitNeedsGoModule(t *testing.T) {
	err := run([]string{"init", "-offline", "-dir", t.TempDir()}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "go mod init") {
		t.Errorf("err = %v, want a hint to run go mod init", err)
	}
}

func TestInitRejectsBufV1(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module x\n", "buf.yaml": "version: v1\n"})
	err := run([]string{"init", "-offline", "-dir", dir}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "buf config migrate") {
		t.Errorf("err = %v", err)
	}
}

// An empty module gets a starter service named after it, a server stub and
// a policy test, so lint, generate and build work from the first run.
func TestInitWritesStarterService(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module github.com/acme/time-counter\n"})
	out := initOffline(t, dir)
	starter := read(t, dir, "proto/acme/time_counter/v1/time_counter.proto")
	for _, want := range []string{
		"package acme.time_counter.v1;",
		"service TimeCounterService {",
		`owner_team: "time-counter-team"`,
		`get: "/timeCounter/v1/{name=items/*}"`,
		`scopes: "time_counter.item.read"`,
		`type: "time-counter.acme.com/Item"`,
	} {
		if !strings.Contains(starter, want) {
			t.Errorf("starter lacks %s", want)
		}
	}
	if !strings.Contains(read(t, dir, "server.go"), "time_counterv1connect.UnimplementedTimeCounterServiceHandler") {
		t.Errorf("server.go:\n%s", read(t, dir, "server.go"))
	}
	if !strings.Contains(read(t, dir, "policy_table_test.go"), "time_counterv1connect.TimeCounterServiceName") {
		t.Error("policy test does not name the starter service")
	}
	main := mainGo(t, dir, out)
	for _, want := range []string{"time_counterv1.TimeCounterServiceMCPTools()", `"writer": {"time_counter.item.read", "time_counter.item.write"}`} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s", want)
		}
	}
	for _, src := range []string{read(t, dir, "server.go"), read(t, dir, "policy_table_test.go"), main} {
		if _, err := format.Source([]byte(src)); err != nil {
			t.Errorf("generated Go does not parse: %v\n%s", err, src)
		}
	}
	// The starter exists now, so a second run writes nothing new.
	before := snapshot(t, dir)
	initOffline(t, dir)
	if !maps(before, snapshot(t, dir)) {
		t.Error("a second dedo init changed files")
	}
}

// Without scopes, the printed main.go says why every call is denied.
func TestInitExplainsMissingScopes(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                     "module example.com/notes\n",
		"proto/notes/v1/notes.proto": "syntax = \"proto3\";\npackage notes.v1;\nservice NoteService {}\n",
	})
	out := initOffline(t, dir)
	if !strings.Contains(mainGo(t, dir, out), "declares no scopes yet") {
		t.Error("main.go does not explain the empty devTokens")
	}
	if _, err := os.Stat(filepath.Join(dir, "proto/example/notes/v1/notes.proto")); err == nil {
		t.Error("a starter was written next to existing protos")
	}
}

// A library repository keeps its package: no server stub or package main test.
func TestInitSkipsServerFilesOutsidePackageMain(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                     "module example.com/notes\n",
		"lib.go":                     "package notes\n",
		"proto/notes/v1/notes.proto": "syntax = \"proto3\";\npackage notes.v1;\nservice NoteService {}\n",
	})
	out := initOffline(t, dir)
	for _, name := range []string{"server.go", "policy_table_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s written into package notes", name)
		}
	}
	if !strings.Contains(out, "package notes, not main") {
		t.Errorf("output does not say why:\n%s", out)
	}
}

// An existing newServer is kept.
func TestInitKeepsExistingServer(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                     "module example.com/notes\n",
		"handlers.go":                "package main\n\nfunc newServer() any { return nil }\n",
		"proto/notes/v1/notes.proto": "syntax = \"proto3\";\npackage notes.v1;\nservice NoteService {}\n",
	})
	initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "server.go")); err == nil {
		t.Error("server.go written beside an existing newServer")
	}
}

// A missing buf.lock queues buf dep update, even when the deps are in place.
func TestInitRestoresMissingLock(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                     "module example.com/notes\n",
		"buf.yaml":                   "version: v2\ndeps:\n  - buf.build/googleapis/googleapis\n  - buf.build/bufbuild/protovalidate\n  - buf.build/r-a/dedo\n",
		"proto/notes/v1/notes.proto": "syntax = \"proto3\";\npackage notes.v1;\n",
	})
	out := initOffline(t, dir)
	if !strings.Contains(out, "buf.lock: missing") || !strings.Contains(out, "  buf dep update") {
		t.Errorf("buf dep update not queued:\n%s", out)
	}
}

// A failed command does not stop the ones after it.
func TestRunCommandsCarriesOn(t *testing.T) {
	var out bytes.Buffer
	in := &initer{p: &project{dir: t.TempDir()}, out: &out, cmds: [][]string{{"false"}, {"true"}, {"false"}}}
	failed := in.runCommands(false)
	if len(failed) != 2 || strings.Count(out.String(), "$ ") != 3 {
		t.Errorf("failed = %v, output:\n%s", failed, out.String())
	}
}

func TestNamesFor(t *testing.T) {
	tests := map[string]starterNames{
		"github.com/acme/time-counter": {Package: "acme.time_counter.v1", Service: "TimeCounterService", Root: "/timeCounter", Domain: "time-counter.acme.com"},
		"example.com/notes":            {Package: "example.notes.v1", Service: "NotesService", Root: "/notes", Domain: "notes.example.com"},
		"github.com/acme/shop-api/v2":  {Package: "acme.shop_api.v1", Service: "ShopApiService", Root: "/shopApi", Domain: "shop-api.acme.com"},
		"notes":                        {Package: "example.notes.v1", Service: "NotesService", Root: "/notes", Domain: "notes.example.com"},
		"gitlab.com/Big-Co/3d-printer": {Package: "big_co.d_printer.v1", Service: "DPrinterService", Root: "/dPrinter", Domain: "d-printer.big-co.com"},
	}
	for module, want := range tests {
		got := namesFor(module)
		if got.Package != want.Package || got.Service != want.Service || got.Root != want.Root || got.Domain != want.Domain {
			t.Errorf("namesFor(%q) = %+v", module, got)
		}
	}
}

func TestGoPackageName(t *testing.T) {
	for pkg, want := range map[string]string{"acme.time_counter.v1": "time_counterv1", "raveesh_me.todo.v1": "todov1", "single": "single"} {
		if got := goPackageName(pkg); got != want {
			t.Errorf("goPackageName(%q) = %q, want %q", pkg, got, want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil || strings.TrimSpace(out.String()) != version {
		t.Errorf("dedo version = %q, %v", out.String(), err)
	}
	if err := run([]string{"bogus"}, &out); err == nil {
		t.Error("an unknown command succeeded")
	}
}

// A repository on managed mode gets the overrides dedo's module needs, after
// its own catch-all override.
func TestInitExtendsManagedMode(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module example.com/shop\n",
		"buf.gen.yaml": `version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: example.com/shop/gen/go
  disable:
    - module: buf.build/googleapis/googleapis
plugins: []
`,
	})
	initOffline(t, dir)
	managed := lookup(yamlAt(t, dir, "buf.gen.yaml"), "managed")
	override := lookup(managed, "override").Content
	if len(override) != 2 || lookup(override[1], "module").Value != optionsModule {
		t.Errorf("override = %v", allStrings(lookup(managed, "override")))
	}
	var disabled []string
	for _, item := range lookup(managed, "disable").Content {
		disabled = append(disabled, lookup(item, "module").Value)
	}
	if !slices.Equal(disabled, []string{googleapis, protovalidate}) {
		t.Errorf("disable = %v", disabled)
	}
}

func TestTemplatesCommand(t *testing.T) {
	var list bytes.Buffer
	if err := run([]string{"templates"}, &list); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"buf.yaml", "buf.gen.yaml", "Makefile.api", "main.go.tmpl", "docs/docs.go.tmpl", "claude.md"} {
		if !slices.Contains(strings.Split(strings.TrimSpace(list.String()), "\n"), want) {
			t.Errorf("dedo templates lacks %s:\n%s", want, list.String())
		}
	}
	var one bytes.Buffer
	if err := run([]string{"templates", "claude.md"}, &one); err != nil || !strings.HasPrefix(one.String(), guidanceMarker) {
		t.Errorf("dedo templates claude.md = %.40q, %v", one.String(), err)
	}
	if err := run([]string{"templates", "nope"}, &one); err == nil {
		t.Error("an unknown template printed")
	}
}

// Upgrading from a release before v0.9.0 removes the old generated schema
// files, which would otherwise duplicate the new ones' declarations.
func TestInitRemovesStaleSchemaFiles(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                               "module example.com/shop\n",
		"gen/go/shop/v1/shop.mcp_schema.pb.go": "package shopv1\n",
		"gen/go/shop/v1/shop.pb.go":            "package shopv1\n",
	})
	out := initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "gen/go/shop/v1/shop.mcp_schema.pb.go")); !os.IsNotExist(err) {
		t.Errorf("stale schema file kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen/go/shop/v1/shop.pb.go")); err != nil {
		t.Errorf("other generated code removed: %v", err)
	}
	if !strings.Contains(out, "shop.mcp.pb.go") {
		t.Errorf("output does not explain the removal:\n%s", out)
	}
}

// main.go is written once; after that the user's file is kept and the
// template is printed beside it for comparison.
func TestInitWritesMainOnce(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/notes\n"})
	out := initOffline(t, dir)
	if !strings.Contains(out, "main.go: created") || !strings.Contains(read(t, dir, "main.go"), "NotesServiceMCPTools()") {
		t.Fatalf("main.go not written:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = initOffline(t, dir)
	if read(t, dir, "main.go") != "package main\n\nfunc main() {}\n" {
		t.Error("the user's main.go was replaced")
	}
	if !strings.Contains(out, "dedo's template, to compare with yours") {
		t.Errorf("template not printed for comparison:\n%s", out)
	}
}

// The lint plugin belongs to the protos, not to a Go module: dedo init
// drops the go.mod pin earlier releases added.
func TestInitDropsLintPin(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                        "module example.com/shop\n\ngo 1.26\n\ntool " + lintPluginPkg + "\n",
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\n",
	})
	if out := initOffline(t, dir); !strings.Contains(out, "go get -tool "+lintPluginPkg+"@none") {
		t.Errorf("output:\n%s", out)
	}
}

// A repository generating with Buf's remote plugins keeps them: dedo adds
// only its own generators, into the same Go output, and main.go imports the
// packages the files' go_package names.
func TestInitFitsRemotePluginsAndGoPackage(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module example.com/shop\n",
		"buf.gen.yaml": `version: v2
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.10
    out: gen
    opt: paths=source_relative
  - remote: buf.build/connectrpc/go
    out: gen
    opt: paths=source_relative
  - local: protoc-gen-go-grpc
    out: gen
inputs:
  - directory: proto
`,
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\noption go_package = \"example.com/shop/gen/shop/cart/v1;cartv1\";\nservice CartService {}\n",
	})
	out := initOffline(t, dir)
	gen := yamlAt(t, dir, "buf.gen.yaml")
	plugins := lookup(gen, "plugins").Content
	count := func(g string) int {
		n := 0
		for _, p := range plugins {
			if runsGenerator(p, g) {
				n++
			}
		}
		return n
	}
	for _, g := range []string{"protoc-gen-go", "protoc-gen-connect-go", "protoc-gen-dedo-mcp", "protoc-gen-dedo-openapi"} {
		if count(g) != 1 {
			t.Errorf("%s runs %d times", g, count(g))
		}
	}
	mcp := findItem(lookup(gen, "plugins"), func(n *yaml.Node) bool { return runsGenerator(n, "protoc-gen-dedo-mcp") })
	if lookup(mcp, "out").Value != "gen" || lookup(mcp, "opt").Value != "paths=source_relative" {
		t.Errorf("protoc-gen-dedo-mcp writes to %s with %v, want gen with paths=source_relative", lookup(mcp, "out").Value, allStrings(lookup(mcp, "opt")))
	}
	if strings.Contains(out, "go get -tool google.golang.org/protobuf/cmd/protoc-gen-go") {
		t.Error("protoc-gen-go pinned although the remote plugin generates it")
	}
	main := mainGo(t, dir, out)
	if !strings.Contains(main, `cartv1 "example.com/shop/gen/shop/cart/v1"`) {
		t.Errorf("main.go does not import the go_package:\n%s", main)
	}
}

// With managed mode, the prefix names the Go packages.
func TestInitUsesManagedGoPrefix(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module example.com/shop\n",
		"buf.gen.yaml": `version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: example.com/shop/internal/pb
plugins: []
`,
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\noption go_package = \"ignored/by/managed;x\";\nservice CartService {}\n",
	})
	out := initOffline(t, dir)
	if main := mainGo(t, dir, out); !strings.Contains(main, `cartv1 "example.com/shop/internal/pb/shop/cart/v1"`) {
		t.Errorf("main.go does not use the managed prefix:\n%s", main)
	}
}

// A repository of protos without Go gets the options module at this
// release and the vendored lint plugin, replacing the BSR plugin earlier
// releases listed, and nothing that needs Go.
func TestInitSchemaRepository(t *testing.T) {
	dir := repo(t, map[string]string{
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\nservice CartService {}\n",
		"buf.yaml":                      "version: v2\nmodules:\n  - path: proto\nplugins:\n  - plugin: " + remotePlugin + ":v0.14.0\n",
	})
	out := initOffline(t, dir)
	buf := read(t, dir, "buf.yaml")
	for _, want := range []string{"plugin: " + lintPlugin, optionsModule + ":" + version} {
		if !strings.Contains(buf, want) {
			t.Errorf("buf.yaml lacks %s:\n%s", want, buf)
		}
	}
	if strings.Contains(buf, remotePlugin) {
		t.Errorf("the BSR plugin, which cannot read rules.yml, is still listed:\n%s", buf)
	}
	if !strings.Contains(read(t, dir, ".gitignore"), ".tools/") {
		t.Error(".tools/ is not ignored")
	}
	for _, name := range []string{"go.mod", "Makefile.api", "buf.gen.yaml", "main.go", "server.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s written into a schema repository", name)
		}
	}
	if !strings.Contains(out, "dedo tools") || !strings.HasPrefix(read(t, dir, "AGENTS.md"), guidanceMarker) {
		t.Errorf("output:\n%s", out)
	}
	// An upgrade moves the options module's label, and so the plugin's.
	if err := os.WriteFile(filepath.Join(dir, "buf.yaml"), []byte(strings.ReplaceAll(buf, version, "v0.1.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	var up bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &up); err != nil {
		t.Fatalf("dedo upgrade: %v\n%s", err, up.String())
	}
	if got := read(t, dir, "buf.yaml"); strings.Contains(got, "v0.1.0") {
		t.Errorf("upgrade left old labels:\n%s", got)
	}
}

// Online, dedo init vendors the global plugin and checks the house rules
// with it.
func TestInitSchemaRepositoryVendorsPlugin(t *testing.T) {
	installed := stubGlobalPlugin(t)
	dir := repo(t, map[string]string{
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\n",
		"buf.yaml":                      "version: v2\nmodules:\n  - path: proto\n",
		"buf.lock":                      "version: v2\n",
	})
	in := &initer{p: &project{dir: dir, schemaOnly: true}, release: version, out: io.Discard}
	if failed := in.vendor(false); len(failed) > 0 {
		t.Fatalf("failed: %v", failed)
	}
	if strings.Join(*installed, " ") != version {
		t.Errorf("installed %v", *installed)
	}
	if v, _ := pluginVersion(filepath.Join(dir, pluginPath())); v != version {
		t.Errorf("vendored %q", v)
	}
}

// buf.yaml lists one buf-plugin-dedo-lint: dedo init replaces one listed at
// another path rather than adding a second, which buf rejects.
func TestInitReplacesOtherPluginPath(t *testing.T) {
	for _, schema := range []bool{false, true} {
		files := map[string]string{
			"buf.yaml":                      "version: v2\nmodules:\n  - path: proto\nplugins:\n  - plugin: ../../dist/buf-plugin-dedo-lint\n",
			"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\n",
		}
		if !schema {
			files["go.mod"] = "module example.com/shop\n"
		}
		dir := repo(t, files)
		initOffline(t, dir)
		var plugins []string
		for _, item := range lookup(yamlAt(t, dir, "buf.yaml"), "plugins").Content {
			plugins = append(plugins, lookup(item, "plugin").Value)
		}
		if !slices.Equal(plugins, []string{lintPlugin}) {
			t.Errorf("schema %v: plugins = %v", schema, plugins)
		}
	}
}

// Services without dedo declarations are named up front, with what fails.
func TestInitNamesUndeclaredServices(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                        "module example.com/shop\n",
		"proto/shop/cart/v1/cart.proto": "syntax = \"proto3\";\npackage shop.cart.v1;\nservice CartService {}\n",
	})
	if out := initOffline(t, dir); !strings.Contains(out, "CartService declare nothing for dedo yet") {
		t.Errorf("no hint about CartService:\n%s", out)
	}
}

// A remote Connect plugin pinned to v1 is flagged: dedo needs v2 code.
func TestInitWarnsOnConnectV1Remote(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":       "module example.com/shop\n",
		"buf.gen.yaml": "version: v2\nplugins:\n  - remote: buf.build/connectrpc/go:v1.19.1\n    out: gen\n",
	})
	if out := initOffline(t, dir); !strings.Contains(out, "generates connect-go v1 code") {
		t.Errorf("no warning:\n%s", out)
	}
}

// buf.yaml lists DEDO of the plugin's rules, and nothing else of them;
// rules.yml chooses which run. A new repository gets dedo's default sets.
func TestInitRuleSet(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":   "module example.com/shop\n",
		"buf.yaml": "version: v2\nlint:\n  use: [STANDARD]\n",
	})
	initOffline(t, dir)
	if got := scalars(lookup(lookup(yamlAt(t, dir, "buf.yaml"), "lint"), "use")); !slices.Equal(got, []string{"STANDARD", "DEDO"}) {
		t.Errorf("lint.use = %v", got)
	}
	if got := scalars(lookup(yamlAt(t, dir, "rules.yml"), "use")); !slices.Equal(got, []string{"AIP_NOLANG", "ZALANDO", "DEDO", "HOUSE"}) {
		t.Errorf("rules.yml use = %v", got)
	}
	if out := initOffline(t, dir); strings.Contains(out, "rules.yml:") || strings.Contains(out, "lint.use") {
		t.Errorf("a second run changed the rule selection:\n%s", out)
	}
}

// Releases before v0.15.0 chose the plugin's rules in buf.yaml. init moves
// that choice to rules.yml: the sets lint.use named become use, the rules
// lint.except named become disable, except those dedo now skips or
// overrides itself, and Buf's own entries stay.
func TestInitMovesSelection(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module example.com/shop\n",
		"buf.yaml": "version: v2\nlint:\n  use: [STANDARD, AIP_CORE, DEDO, ZALANDO, HOUSE]\n  except:\n" +
			"    # Every response is a unique <Rpc>Response.\n    - AIP_0131_RESPONSE_MESSAGE_NAME\n" +
			"    - AIP_0192_HAS_COMMENTS\n    - DEDO_NO_JSON_NAME\n    - ENUM_VALUE_PREFIX\n",
		"rules.yml": "rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: X.}\n",
	})
	out := initOffline(t, dir)
	lint := lookup(yamlAt(t, dir, "buf.yaml"), "lint")
	if got := scalars(lookup(lint, "use")); !slices.Equal(got, []string{"STANDARD", "DEDO"}) {
		t.Errorf("lint.use = %v", got)
	}
	if got := scalars(lookup(lint, "except")); !slices.Equal(got, []string{"ENUM_VALUE_PREFIX"}) {
		t.Errorf("lint.except = %v", got)
	}
	rules := yamlAt(t, dir, "rules.yml")
	if got := scalars(lookup(rules, "use")); !slices.Equal(got, []string{"AIP_CORE", "ZALANDO", "HOUSE", "DEDO"}) {
		t.Errorf("rules.yml use = %v", got)
	}
	if got := scalars(lookup(rules, "disable")); !slices.Equal(got, []string{"DEDO_NO_JSON_NAME"}) {
		t.Errorf("rules.yml disable = %v", got)
	}
	if lookup(rules, "rules") == nil {
		t.Error("the house rules are gone")
	}
	if !strings.Contains(out, "rules.yml: disable gained DEDO_NO_JSON_NAME") {
		t.Errorf("output: %s", out)
	}
}

// A comment that heads the except list stays when its first rule goes.
func TestDropScalarsKeepsListHeader(t *testing.T) {
	doc, err := parseYAML([]byte("except:\n  # Skipped on purpose; RULES.md says why.\n  # Every response is wrapped.\n  - AIP_0131_RESPONSE_MESSAGE_NAME\n  - AIP_0192_HAS_COMMENTS\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dropScalars(lookup(root(doc), "except"), overriddenRules...); len(got) != 1 {
		t.Fatalf("dropped %v", got)
	}
	b, err := encodeYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if out := string(b); !strings.Contains(out, "# Skipped on purpose") || strings.Contains(out, "wrapped") {
		t.Errorf("buf.yaml:\n%s", out)
	}
}

// A service that declares tenancy gets the DEDO_MULTI_TENANT rules and a
// main.go that checks tenants; any other keeps both as they were.
func TestInitMultiTenant(t *testing.T) {
	const tenantProto = `syntax = "proto3";

package acme.contacts.v1;

import "dedo/v1/options.proto";

service ContactService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "contacts-team"
    tenancy: {collection: "locations"}
  };
}
`
	for _, tt := range []struct {
		name    string
		bufYAML string // "" when dedo init writes buf.yaml
	}{
		{"new buf.yaml", ""},
		{"existing lint section", "version: v2\nmodules:\n  - path: proto\nlint:\n  use: [STANDARD]\n"},
		{"buf.yaml without lint", "version: v2\nmodules:\n  - path: proto\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				"go.mod":                                "module example.com/contacts\n",
				"proto/acme/contacts/v1/contacts.proto": tenantProto,
			}
			if tt.bufYAML != "" {
				files["buf.yaml"] = tt.bufYAML
			}
			dir := repo(t, files)
			out := initOffline(t, dir)
			if !slices.Contains(scalars(lookup(yamlAt(t, dir, "rules.yml"), "use")), "DEDO_MULTI_TENANT") {
				t.Errorf("DEDO_MULTI_TENANT missing from rules.yml's use:\n%s", read(t, dir, "rules.yml"))
			}
			main := mainGo(t, dir, out)
			if formatted, err := format.Source([]byte(main)); err != nil || string(formatted) != main {
				t.Fatalf("main.go is not gofmt-formatted (%v):\n%s", err, main)
			}
			for _, want := range []string{"Tenants:       tenantAllowed,", `"slices"`, "func tenantAllowed(", "devTenants[token]"} {
				if !strings.Contains(main, want) {
					t.Errorf("main.go lacks %s", want)
				}
			}
		})
	}

	dir := repo(t, map[string]string{
		"go.mod":                                "module example.com/contacts\n",
		"proto/acme/contacts/v1/contacts.proto": strings.Replace(tenantProto, `    tenancy: {collection: "locations"}`+"\n", "", 1),
	})
	out := initOffline(t, dir)
	if slices.Contains(scalars(lookup(yamlAt(t, dir, "rules.yml"), "use")), "DEDO_MULTI_TENANT") {
		t.Error("DEDO_MULTI_TENANT added without tenancy")
	}
	if main := mainGo(t, dir, out); strings.Contains(main, "tenant") || strings.Contains(main, `"slices"`) {
		t.Errorf("main.go of a single-tenant service mentions tenants:\n%s", main)
	}
}

// dedo tools fetches the git imports of rules.yml, so the plugin, which
// never touches the network, can read them.
func TestFetchRules(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	remote := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."}, {"commit", "--quiet", "-m", "v1"}, {"tag", "v1"}} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(remote, "rules.yml"), []byte("rules:\n  - {id: HOUSE_SHARED, on: field, check: \"true\", message: Shared.}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("git", append([]string{"-C", remote, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	dir := repo(t, map[string]string{
		"buf.yaml":  "version: v2\n",
		"rules.yml": "imports:\n  - git: " + remote + "\n    ref: v1\n",
	})
	var out bytes.Buffer
	// No plugin is built here, so consolidating fails after the fetch.
	if err := houseRules(dir, &out); err == nil || !strings.Contains(err.Error(), "run dedo tools") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), "fetched house rules "+remote+"@v1") {
		t.Errorf("output: %s", out.String())
	}
}
