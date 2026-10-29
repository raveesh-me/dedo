package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
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
		if err := generate(plugin, f); err != nil {
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
