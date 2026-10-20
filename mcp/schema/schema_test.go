package schema

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const todoProto = "raveesh_me/todo/v1/todo.proto"

// methods compiles the todo example the way buf would, through an encoded
// plugin request so custom options arrive resolved, and returns its RPCs.
func methods(t *testing.T) map[string]*protogen.Method {
	t.Helper()
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{
				"../../examples/todo/proto",
				"../../examples/todo/third_party/dedo",
				"../../openapi/restdoc/testdata/deps",
			},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), todoProto)
	if err != nil {
		t.Fatal(err)
	}
	req := &pluginpb.CodeGeneratorRequest{FileToGenerate: []string{todoProto}}
	seen := map[string]bool{}
	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		for i := 0; i < fd.Imports().Len(); i++ {
			add(fd.Imports().Get(i).FileDescriptor)
		}
		fdp := protodesc.ToFileDescriptorProto(fd)
		if fdp.Options == nil {
			fdp.Options = &descriptorpb.FileOptions{}
		}
		fdp.Options.GoPackage = proto.String("example.com/gen/" + strings.TrimSuffix(fd.Path(), ".proto"))
		req.ProtoFile = append(req.ProtoFile, fdp)
	}
	add(files[0])
	b, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(b, decoded); err != nil {
		t.Fatal(err)
	}
	plugin, err := protogen.Options{}.New(decoded)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*protogen.Method{}
	for _, f := range plugin.Files {
		for _, svc := range f.Services {
			for _, m := range svc.Methods {
				out[string(m.Desc.Name())] = m
			}
		}
	}
	return out
}

// compiled checks the schema is valid JSON Schema with jsonschema-go, the
// library the MCP Go SDK validates tool arguments with, and returns it.
func compiled(t *testing.T, name string, s Object) *jsonschema.Resolved {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var sch jsonschema.Schema
	if err := json.Unmarshal(b, &sch); err != nil {
		t.Fatal(err)
	}
	resolved, err := sch.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		t.Fatalf("%s: not a valid schema: %v", name, err)
	}
	return resolved
}

func validate(t *testing.T, sch *jsonschema.Resolved, args string) error {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		t.Fatal(err)
	}
	return sch.Validate(v)
}

func props(s Object, path ...string) Object {
	for _, p := range path {
		s = s["properties"].(Object)[p].(Object)
	}
	return s
}

// A partial update needs only the resource name: the mask says what changes.
func TestUpdateIsPartial(t *testing.T) {
	s := Input(methods(t)["UpdateTodo"])
	sch := compiled(t, "update", s)
	todo := props(s, "todo")
	if req, _ := todo["required"].([]string); len(req) != 1 || req[0] != "name" {
		t.Errorf("todo.required = %v, want [name]", todo["required"])
	}
	if _, ok := todo["properties"].(Object)["create_time"]; ok {
		t.Error("OUTPUT_ONLY create_time is an input")
	}
	mask := props(s, "update_mask")["description"].(string)
	if !strings.Contains(mask, "Valid paths: title, done, budgetAmount, assigneeEmail.") {
		t.Errorf("update_mask description = %q", mask)
	}
	if err := validate(t, sch, `{"todo":{"name":"todos/1","done":true},"update_mask":"done"}`); err != nil {
		t.Errorf("marking done without a title: %v", err)
	}
	if err := validate(t, sch, `{"todo":{"done":true}}`); err == nil {
		t.Error("an update without the to-do's name validated")
	}
}

// A create leaves out what the server sets: the name and output-only fields.
func TestCreateLeavesOutServerFields(t *testing.T) {
	s := Input(methods(t)["CreateTodo"])
	sch := compiled(t, "create", s)
	todo := props(s, "todo")
	for _, f := range []string{"name", "create_time"} {
		if _, ok := todo["properties"].(Object)[f]; ok {
			t.Errorf("%s is a create input", f)
		}
	}
	if req, _ := todo["required"].([]string); len(req) != 1 || req[0] != "title" {
		t.Errorf("todo.required = %v, want [title]", todo["required"])
	}
	if err := validate(t, sch, `{"todo":{"title":"x","budget_amount":{"currency_code":"EUR","units":"5"}},"todo_id":"a"}`); err != nil {
		t.Errorf("valid create: %v", err)
	}
	if err := validate(t, sch, `{"todo":{"done":true}}`); err == nil {
		t.Error("a create without a title validated")
	}
}

// Every input field carries its comment, so an agent knows what to send.
func TestFieldsAreDescribed(t *testing.T) {
	for name, m := range methods(t) {
		if m.Desc.IsStreamingServer() {
			continue
		}
		s := Input(m)
		compiled(t, name, s)
		var walk func(path string, s Object)
		walk = func(path string, s Object) {
			p, _ := s["properties"].(Object)
			for field, v := range p {
				fs := v.(Object)
				if d, _ := fs["description"].(string); d == "" {
					t.Errorf("%s: %s%s has no description", name, path, field)
				}
				walk(path+field+".", fs)
			}
		}
		walk("", s)
		if d, _ := s["description"].(string); d == "" {
			t.Errorf("%s: input has no description", name)
		}
	}
}
