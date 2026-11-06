package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// pluginEnv makes the test binary run the plugin's main instead of the
// tests, so a test sees exactly what buf gets from the plugin.
const pluginEnv = "DEDO_TEST_RUN_PLUGIN"

func TestMain(m *testing.M) {
	if os.Getenv(pluginEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// execPlugin runs protoc-gen-dedo-mcp over the files with params, as buf
// does, and returns its response and stderr.
func execPlugin(t *testing.T, params string, files ...*descriptorpb.FileDescriptorProto) (*pluginpb.CodeGeneratorResponse, string, error) {
	t.Helper()
	var names []string
	for _, f := range files {
		names = append(names, f.GetName())
	}
	req, err := proto.Marshal(&pluginpb.CodeGeneratorRequest{FileToGenerate: names, ProtoFile: files, Parameter: proto.String(params)})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), pluginEnv+"=1")
	cmd.Stdin = bytes.NewReader(req)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, stderr.String(), err
	}
	var resp pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("plugin error: %s", resp.GetError())
	}
	return &resp, stderr.String(), nil
}

// audienceFiles are four services, one per file: component-internal,
// business-unit-internal, with no audience, and public.
func audienceFiles() []*descriptorpb.FileDescriptorProto {
	return []*descriptorpb.FileDescriptorProto{
		withAudience(serviceFile("acme/v1/a.proto", "AlphaService"), dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL),
		withAudience(serviceFile("acme/v1/b.proto", "BetaService"), dedov1.Audience_AUDIENCE_BUSINESS_UNIT_INTERNAL),
		serviceFile("acme/v1/c.proto", "GammaService"),
		withAudience(serviceFile("acme/v1/d.proto", "DeltaService"), dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC),
	}
}

// What the plugin writes under each min_audience, from its parameter as
// buf passes it beside paths=source_relative: each service still has its
// <Service>MCPTools, which returns its tool only when the service is at
// least as wide as the minimum. By default only the component-internal
// service is left out, and a service with no audience always stays.
func TestPluginMinAudience(t *testing.T) {
	for _, c := range []struct {
		min    string
		served map[string]bool
	}{
		{"", map[string]bool{"AlphaService": false, "BetaService": true, "GammaService": true, "DeltaService": true}},
		{"COMPONENT_INTERNAL", map[string]bool{"AlphaService": true, "BetaService": true, "GammaService": true, "DeltaService": true}},
		{"BUSINESS_UNIT_INTERNAL", map[string]bool{"AlphaService": false, "BetaService": true, "GammaService": true, "DeltaService": true}},
		{"COMPANY_INTERNAL", map[string]bool{"AlphaService": false, "BetaService": false, "GammaService": true, "DeltaService": true}},
		{"AUDIENCE_EXTERNAL_PUBLIC", map[string]bool{"AlphaService": false, "BetaService": false, "GammaService": true, "DeltaService": true}},
	} {
		t.Run("min_audience="+c.min, func(t *testing.T) {
			params := "paths=source_relative"
			if c.min != "" {
				params += ",min_audience=" + c.min
			}
			resp, stderr, err := execPlugin(t, params, audienceFiles()...)
			if err != nil {
				t.Fatalf("plugin: %v\n%s", err, stderr)
			}
			files := map[string]string{}
			for _, f := range resp.File {
				files[f.GetName()] = f.GetContent()
			}
			for file, svc := range map[string]string{"acme/v1/a.mcp.pb.go": "AlphaService", "acme/v1/b.mcp.pb.go": "BetaService", "acme/v1/c.mcp.pb.go": "GammaService", "acme/v1/d.mcp.pb.go": "DeltaService"} {
				src, ok := files[file]
				if !ok {
					t.Fatalf("%s not written; wrote %v", file, resp.File)
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ParseComments)
				if err != nil {
					t.Fatalf("%s is not valid Go: %v\n%s", file, err, src)
				}
				tools, schemas := 0, 0
				if c.served[svc] {
					tools, schemas = 1, 2
				}
				if got := toolCount(t, parsed, svc); got != tools {
					t.Errorf("%sMCPTools returns %d tools, want %d", svc, got, tools)
				}
				if got := len(declared(parsed)) - 1; got != schemas {
					t.Errorf("%s declares %d schema maps, want %d: %v", file, got, schemas, declared(parsed))
				}
			}
		})
	}
}

// A run that leaves out every service writes, for each, a <Service>MCPTools
// that returns no tools and nothing else, so main.go keeps compiling.
func TestPluginLeavesOutEveryService(t *testing.T) {
	resp, stderr, err := execPlugin(t, "paths=source_relative", audienceFiles()[0])
	if err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr)
	}
	if len(resp.File) != 1 || resp.File[0].GetName() != "acme/v1/a.mcp.pb.go" {
		t.Fatalf("wrote %v, want acme/v1/a.mcp.pb.go", resp.File)
	}
	got := resp.File[0].GetContent()
	golden := filepath.Join("testdata", "none.mcp.pb.go.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s; run go test -update and review the diff:\n%s", golden, got)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "a.mcp.pb.go", got, 0)
	if err != nil {
		t.Fatal(err)
	}
	// It type-checks, with mcpserver.Tool the type it names.
	if _, err := (&types.Config{Importer: mcpserverStub{}}).Check("example.com/acme/v1", fset, []*ast.File{parsed}, nil); err != nil {
		t.Errorf("does not compile: %v", err)
	}
	if d := declared(parsed); len(d) != 1 || d[0] != "AlphaServiceMCPTools" {
		t.Errorf("declares %v, want only AlphaServiceMCPTools", d)
	}
	if toolCount(t, parsed, "AlphaService") != 0 {
		t.Error("AlphaServiceMCPTools returns tools")
	}
}

// An unknown min_audience fails the run, naming the value.
func TestPluginRejectsUnknownAudience(t *testing.T) {
	_, stderr, err := execPlugin(t, "min_audience=EVERYONE", audienceFiles()...)
	if err == nil || !strings.Contains(stderr, `"EVERYONE"`) {
		t.Errorf("err = %v, stderr %q; want a failure naming EVERYONE", err, stderr)
	}
}

// mcpserverStub imports a package mcpserver declaring the type Tool, as the
// real one does, for type-checking generated code without loading it.
type mcpserverStub struct{}

func (mcpserverStub) Import(path string) (*types.Package, error) {
	if path != "github.com/raveesh-me/dedo/mcp/mcpserver" {
		return nil, fmt.Errorf("unexpected import %s", path)
	}
	pkg := types.NewPackage(path, "mcpserver")
	tool := types.NewTypeName(token.NoPos, pkg, "Tool", nil)
	types.NewNamed(tool, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(tool)
	pkg.MarkComplete()
	return pkg, nil
}
