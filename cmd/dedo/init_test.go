package main

import (
	"bytes"
	"go/format"
	"os"
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
	if got := scalars(lookup(buf, "deps")); !slices.Contains(got, optionsModule) {
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
	if !strings.Contains(read(t, dir, "Makefile.api"), "DEDO_VERSION ?= "+version) {
		t.Error("Makefile.api does not pin this release")
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
		"make tools",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	main := out[strings.Index(out, "// ---- main.go ----")+len("// ---- main.go ----\n"):]
	formatted, err := format.Source([]byte(main))
	if err != nil {
		t.Fatalf("main.go does not parse: %v\n%s", err, main)
	}
	if string(formatted) != main {
		t.Errorf("main.go is not gofmt-formatted:\n%s", main)
	}
	for _, want := range []string{
		`time_counterv1 "github.com/acme/time-counter/gen/go/acme/time_counter/v1"`,
		`time_counterv1.File_acme_time_counter_v1_time_counter_proto.Services().ByName("CounterService")`,
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
	if got := scalars(lookup(buf, "deps")); !slices.Equal(got, []string{"buf.build/acme/common", googleapis, protovalidate, optionsModule}) {
		t.Errorf("deps = %v", got)
	}
	if got := scalars(lookup(lookup(buf, "lint"), "use")); !slices.Equal(got, []string{"DEFAULT", "AIP_NOLANG", "DEDO", "ZALANDO"}) {
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

// Without a service, main.go still parses, with placeholders.
func TestInitWithoutProtos(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/empty\n"})
	out := initOffline(t, dir)
	main := out[strings.Index(out, "// ---- main.go ----")+len("// ---- main.go ----\n"):]
	if _, err := format.Source([]byte(main)); err != nil {
		t.Fatalf("main.go does not parse: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "policy_table_test.go")); err == nil {
		t.Error("policy_table_test.go written without a service")
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
