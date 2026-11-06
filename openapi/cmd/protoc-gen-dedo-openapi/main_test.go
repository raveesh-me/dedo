package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/pb33f/libopenapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

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

const (
	storefront = "acme/storefront/v1/storefront.proto" // component- and business-unit-internal services
	catalog    = "acme/catalog/v1/catalog.proto"       // a service with no audience
	orders     = "acme/orders/v1/orders.proto"         // two services with no audience
)

// request compiles protos from restdoc's test data, as buf would, into a
// plugin request with params.
func request(t *testing.T, params string, protos ...string) []byte {
	t.Helper()
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{"../../restdoc/testdata/domains", "../../restdoc/testdata/deps", "../../../proto"},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), protos...)
	if err != nil {
		t.Fatal(err)
	}
	req := &pluginpb.CodeGeneratorRequest{FileToGenerate: protos, Parameter: proto.String(params)}
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
		// Managed mode gives every file a Go package; protogen needs one.
		fdp.Options.GoPackage = proto.String("example.com/gen/" + strings.TrimSuffix(fd.Path(), ".proto"))
		req.ProtoFile = append(req.ProtoFile, fdp)
	}
	for _, f := range files {
		add(f)
	}
	b, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// runPlugin runs protoc-gen-dedo-openapi on the request and returns its
// response and what it wrote to stderr; it fails when the plugin does.
func runPlugin(t *testing.T, req []byte) (*pluginpb.CodeGeneratorResponse, string) {
	t.Helper()
	resp, stderr, err := execPlugin(t, req)
	if err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr)
	}
	return resp, stderr
}

// execPlugin runs protoc-gen-dedo-openapi on the request.
func execPlugin(t *testing.T, req []byte) (*pluginpb.CodeGeneratorResponse, string, error) {
	t.Helper()
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
	return &resp, stderr.String(), nil
}

// document is the part of the written document the tests read.
type document struct {
	Info struct {
		Title string `json:"title"`
	} `json:"info"`
	Paths map[string]json.RawMessage `json:"paths"`
	Tags  []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

// generated runs the plugin and returns the one document it writes,
// checked to be valid OpenAPI, and its stderr.
func generated(t *testing.T, params string, protos ...string) (document, string) {
	t.Helper()
	resp, stderr := runPlugin(t, request(t, params, protos...))
	if resp.Error != nil {
		t.Fatalf("plugin error: %s", resp.GetError())
	}
	if len(resp.File) != 1 {
		t.Fatalf("wrote %d files, want 1", len(resp.File))
	}
	content := resp.File[0].GetContent()
	parsed, err := libopenapi.NewDocument([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.BuildV3Model(); err != nil {
		t.Fatalf("invalid OpenAPI: %v\n%s", err, content)
	}
	var doc document
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	return doc, stderr
}

func (d document) paths() []string {
	out := make([]string, 0, len(d.Paths))
	for p := range d.Paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (d document) tags() []string {
	var out []string
	for _, t := range d.Tags {
		out = append(out, t.Name)
	}
	return out
}

// What the plugin writes under each min_audience, from its parameter as
// buf passes it: services narrower than it are left out, by default only
// component-internal ones, and a service with no audience always stays.
func TestPluginMinAudience(t *testing.T) {
	shop := []string{"/shop/v1/orders/{ordersId}", "/shop/v1/products/{productsId}", "/shop/v1/returns/{returnsId}"}
	for _, c := range []struct {
		params    string
		paths     []string
		leftOut   []string
		storeTags []string
	}{
		{"title=Shop API", append(slices.Clone(shop), "/shop/v1/stock/{stockId}"), []string{"Product pages"}, []string{"Stock"}},
		{"title=Shop API,min_audience=COMPONENT_INTERNAL", append(slices.Clone(shop), "/shop/v1/productPages/{productPagesId}", "/shop/v1/stock/{stockId}"), nil, []string{"Product pages", "Stock"}},
		{"title=Shop API,min_audience=BUSINESS_UNIT_INTERNAL", append(slices.Clone(shop), "/shop/v1/stock/{stockId}"), []string{"Product pages"}, []string{"Stock"}},
		{"title=Shop API,min_audience=AUDIENCE_EXTERNAL_PUBLIC", shop, []string{"Product pages", "Stock"}, nil},
	} {
		t.Run(c.params, func(t *testing.T) {
			doc, stderr := generated(t, c.params, storefront, catalog, orders)
			want := slices.Clone(c.paths)
			sort.Strings(want)
			if got := doc.paths(); !slices.Equal(got, want) {
				t.Errorf("paths = %q, want %q", got, want)
			}
			tags := doc.tags()
			for _, tag := range c.storeTags {
				if !slices.Contains(tags, tag) {
					t.Errorf("tags %q lack %s", tags, tag)
				}
			}
			for _, tag := range c.leftOut {
				if slices.Contains(tags, tag) {
					t.Errorf("tags %q have %s, which min_audience leaves out", tags, tag)
				}
			}
			// The services without an audience stay whatever the minimum.
			for _, tag := range []string{"Product", "Orders", "Return"} {
				if !slices.Contains(tags, tag) {
					t.Errorf("tags %q lack %s, a service with no audience", tags, tag)
				}
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want nothing", stderr)
			}
		})
	}
}

// A run that leaves out every service writes a valid document with no
// operations, and warns on stderr, which buf shows, without failing.
func TestPluginLeavesOutEveryService(t *testing.T) {
	doc, stderr := generated(t, "min_audience=EXTERNAL_PUBLIC", storefront)
	if len(doc.Paths) != 0 || len(doc.Tags) != 0 {
		t.Errorf("paths %q, tags %q; want none", doc.paths(), doc.tags())
	}
	if !strings.Contains(stderr, "protoc-gen-dedo-openapi: warning: ") || !strings.Contains(stderr, "min_audience") {
		t.Errorf("stderr = %q, want a warning naming min_audience", stderr)
	}
}

// An unknown min_audience fails the run, naming the value.
func TestPluginRejectsUnknownAudience(t *testing.T) {
	resp, stderr, err := execPlugin(t, request(t, "min_audience=EVERYONE", storefront))
	if err == nil || !strings.Contains(stderr, `"EVERYONE"`) {
		t.Errorf("err = %v, stderr %q, response %v; want a failure naming EVERYONE", err, stderr, resp)
	}
}
