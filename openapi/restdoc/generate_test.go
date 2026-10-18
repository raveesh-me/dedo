package restdoc

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/pb33f/libopenapi"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const libraryProto = "raveesh_me/library/v1/library.proto"

// request compiles the library example the way buf would and returns the
// plugin request, encoded and decoded like a real plugin invocation so that
// custom options arrive as resolved extensions.
func request(t *testing.T, params string) *pluginpb.CodeGeneratorRequest {
	t.Helper()
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{"../../examples/library/proto", "../../proto", "testdata/deps"},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), libraryProto)
	if err != nil {
		t.Fatal(err)
	}
	req := &pluginpb.CodeGeneratorRequest{FileToGenerate: []string{libraryProto}, Parameter: proto.String(params)}
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
		if fdp.GetOptions() == nil {
			fdp.Options = &descriptorpb.FileOptions{}
		}
		fdp.Options.GoPackage = proto.String("example.com/gen/" + strings.ReplaceAll(fd.Path(), ".proto", ""))
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
	return decoded
}

func generate(t *testing.T, params string) string {
	t.Helper()
	var cfg Config
	plugin, err := protogen.Options{ParamFunc: cfg.Set}.New(request(t, params))
	if err != nil {
		t.Fatal(err)
	}
	if err := Generate(plugin, cfg); err != nil {
		t.Fatal(err)
	}
	res := plugin.Response()
	if res.Error != nil {
		t.Fatal(res.GetError())
	}
	if len(res.File) != 1 {
		t.Fatalf("got %d files, want 1", len(res.File))
	}
	return res.File[0].GetContent()
}

func TestGenerateLibraryGolden(t *testing.T) {
	got := generate(t, "title=Library API,server=https://api.example.com,filename=library.openapi.json")
	golden := filepath.Join("testdata", "library.openapi.json")
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

	doc, err := libopenapi.NewDocument([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	get, ok := model.Model.Paths.PathItems.Get("/v1/shelves/{shelf}/books/{book}")
	if !ok || get.Get == nil || get.Delete == nil {
		t.Fatal("missing GET and DELETE /v1/shelves/{shelf}/books/{book}")
	}
	if get.Get.Summary != "Get a book" || len(get.Get.Parameters) != 2 {
		t.Errorf("GetBook: summary %q, %d params", get.Get.Summary, len(get.Get.Parameters))
	}
	for path := range model.Model.Paths.PathItems.KeysFromOldest() {
		if strings.Contains(path, "LibraryService") {
			t.Errorf("Connect route %q documented", path)
		}
	}
	for _, want := range []string{`"x-sensitive": true`, `"x-error-reasons"`, `"x-destructive": true`, `"Book"`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s", want)
		}
	}
}

func TestConfigRejectsUnknownParameter(t *testing.T) {
	var cfg Config
	if err := cfg.Set("bogus", "x"); err == nil {
		t.Fatal("want error")
	}
}
