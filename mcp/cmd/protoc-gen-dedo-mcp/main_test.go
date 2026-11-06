package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// serviceFile is a proto file in package acme.v1 with one service of one
// unary RPC.
func serviceFile(name, service string) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String(name),
		Package: proto.String("acme.v1"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/acme/v1;acmev1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Get" + service + "Request")},
			{Name: proto.String("Get" + service + "Response")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String(service),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Get" + service),
				InputType:  proto.String(".acme.v1.Get" + service + "Request"),
				OutputType: proto.String(".acme.v1.Get" + service + "Response"),
			}},
		}},
	}
}

// Two proto files of one Go package, each with a service, must not declare
// the same package-level name: the package would not compile.
func TestSchemaMapsAreNamedPerService(t *testing.T) {
	files := []*descriptorpb.FileDescriptorProto{
		serviceFile("acme/v1/a.proto", "AlphaService"),
		serviceFile("acme/v1/b.proto", "BetaService"),
	}
	plugin, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"acme/v1/a.proto", "acme/v1/b.proto"},
		ProtoFile:      files,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plugin.Files {
		if err := generate(plugin, f, Config{}); err != nil {
			t.Fatal(err)
		}
	}
	resp := plugin.Response()
	if resp.Error != nil {
		t.Fatal(resp.GetError())
	}
	declared := map[string]string{}
	for _, out := range resp.File {
		for _, line := range strings.Split(out.GetContent(), "\n") {
			name, ok := strings.CutPrefix(line, "var ")
			if !ok {
				continue
			}
			name, _, _ = strings.Cut(name, " ")
			if prev, dup := declared[name]; dup {
				t.Errorf("%s declared in both %s and %s", name, prev, out.GetName())
			}
			declared[name] = out.GetName()
		}
	}
	for _, want := range []string{"AlphaServiceMCPInputSchemas", "AlphaServiceMCPOutputSchemas", "BetaServiceMCPInputSchemas", "BetaServiceMCPOutputSchemas"} {
		if _, ok := declared[want]; !ok {
			t.Errorf("%s not declared; declared: %v", want, declared)
		}
	}
}

// withAudience gives the file's service the audience a.
func withAudience(fd *descriptorpb.FileDescriptorProto, a dedov1.Audience) *descriptorpb.FileDescriptorProto {
	opts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(opts, dedov1.E_Service, &dedov1.ServiceRules{Audience: a, OwnerTeam: "acme"})
	fd.Service[0].Options = opts
	return fd
}

// generated runs the generator with cfg over the files and returns the Go
// it writes, parsed, by file name.
func generated(t *testing.T, cfg Config, files ...*descriptorpb.FileDescriptorProto) map[string]*ast.File {
	t.Helper()
	var names []string
	for _, f := range files {
		names = append(names, f.GetName())
	}
	plugin, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{FileToGenerate: names, ProtoFile: files})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plugin.Files {
		if err := generate(plugin, f, cfg); err != nil {
			t.Fatal(err)
		}
	}
	resp := plugin.Response()
	if resp.Error != nil {
		t.Fatal(resp.GetError())
	}
	out := map[string]*ast.File{}
	for _, f := range resp.File {
		parsed, err := parser.ParseFile(token.NewFileSet(), f.GetName(), f.GetContent(), parser.ParseComments)
		if err != nil {
			t.Fatalf("%s is not valid Go: %v\n%s", f.GetName(), err, f.GetContent())
		}
		out[f.GetName()] = parsed
	}
	return out
}

// declared lists a file's top-level functions and variables.
func declared(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			out = append(out, d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if v, ok := s.(*ast.ValueSpec); ok {
					for _, n := range v.Names {
						out = append(out, n.Name)
					}
				}
			}
		}
	}
	return out
}

// toolCount is how many tools <service>MCPTools returns: the elements of
// its composite literal, or 0 when it returns nil.
func toolCount(t *testing.T, f *ast.File, service string) int {
	t.Helper()
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != service+"MCPTools" {
			continue
		}
		ret := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt).Results[0]
		if lit, ok := ret.(*ast.CompositeLit); ok {
			return len(lit.Elts)
		}
		if id, ok := ret.(*ast.Ident); ok && id.Name == "nil" {
			return 0
		}
		t.Fatalf("%sMCPTools returns %T", service, ret)
	}
	t.Fatalf("%sMCPTools not declared", service)
	return 0
}

// A service below min_audience still has <Service>MCPTools, which returns
// no tools, and no schema maps: main.go registers every service's tools and
// keeps compiling. The default leaves out component-internal services only;
// a service without an audience is always kept, since lint rejects it.
func TestMinAudience(t *testing.T) {
	files := func() []*descriptorpb.FileDescriptorProto {
		return []*descriptorpb.FileDescriptorProto{
			withAudience(serviceFile("acme/v1/a.proto", "AlphaService"), dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL),
			withAudience(serviceFile("acme/v1/b.proto", "BetaService"), dedov1.Audience_AUDIENCE_BUSINESS_UNIT_INTERNAL),
			serviceFile("acme/v1/c.proto", "GammaService"),
		}
	}
	for _, c := range []struct {
		name   string
		min    dedov1.Audience
		served map[string]bool
	}{
		{"default", dedov1.Audience_AUDIENCE_UNSPECIFIED, map[string]bool{"AlphaService": false, "BetaService": true, "GammaService": true}},
		{"component internal", dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL, map[string]bool{"AlphaService": true, "BetaService": true, "GammaService": true}},
		{"external public", dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC, map[string]bool{"AlphaService": false, "BetaService": false, "GammaService": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := generated(t, Config{MinAudience: c.min}, files()...)
			for file, svc := range map[string]string{"example.com/acme/v1/a.mcp.pb.go": "AlphaService", "example.com/acme/v1/b.mcp.pb.go": "BetaService", "example.com/acme/v1/c.mcp.pb.go": "GammaService"} {
				f, ok := out[file]
				if !ok {
					t.Fatalf("%s not written; wrote %v", file, out)
				}
				want := []string{svc + "MCPTools"}
				tools := 0
				if c.served[svc] {
					want = []string{svc + "MCPInputSchemas", svc + "MCPOutputSchemas", svc + "MCPTools"}
					tools = 1
				}
				if got := declared(f); !slices.Equal(got, want) {
					t.Errorf("%s declares %v, want %v", file, got, want)
				}
				if got := toolCount(t, f, svc); got != tools {
					t.Errorf("%sMCPTools returns %d tools, want %d", svc, got, tools)
				}
			}
		})
	}
}

// min_audience arrives as a plugin parameter beside protogen's own.
func TestConfigParameters(t *testing.T) {
	request := func(params string) *pluginpb.CodeGeneratorRequest {
		return &pluginpb.CodeGeneratorRequest{
			FileToGenerate: []string{"acme/v1/a.proto"},
			ProtoFile:      []*descriptorpb.FileDescriptorProto{serviceFile("acme/v1/a.proto", "AlphaService")},
			Parameter:      proto.String(params),
		}
	}
	for _, c := range []struct {
		params string
		want   dedov1.Audience
	}{
		{"paths=source_relative", dedov1.Audience_AUDIENCE_UNSPECIFIED},
		{"paths=source_relative,min_audience=COMPANY_INTERNAL", dedov1.Audience_AUDIENCE_COMPANY_INTERNAL},
		{"min_audience=AUDIENCE_EXTERNAL_PUBLIC", dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC},
	} {
		var cfg Config
		if _, err := (protogen.Options{ParamFunc: cfg.Set}).New(request(c.params)); err != nil {
			t.Errorf("%s: %v", c.params, err)
			continue
		}
		if cfg.MinAudience != c.want {
			t.Errorf("%s: MinAudience = %v, want %v", c.params, cfg.MinAudience, c.want)
		}
	}
	for _, params := range []string{"min_audience=EVERYONE", "min_audience=UNSPECIFIED", "min_audience=company_internal", "bogus=1"} {
		var cfg Config
		if _, err := (protogen.Options{ParamFunc: cfg.Set}).New(request(params)); err == nil {
			t.Errorf("%s: want an error", params)
		}
	}
}

// An unknown audience is an error that names it and lists the known ones.
func TestParseAudienceRejectsUnknown(t *testing.T) {
	_, err := parseAudience("EVERYONE")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"min_audience", `"EVERYONE"`, "COMPONENT_INTERNAL, BUSINESS_UNIT_INTERNAL, COMPANY_INTERNAL, EXTERNAL_PARTNER, EXTERNAL_PUBLIC"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
