package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// documented is serviceFile with a tool description and a declared error
// on its RPC.
func documented(name, service string) *descriptorpb.FileDescriptorProto {
	fd := serviceFile(name, service)
	opts := &descriptorpb.MethodOptions{}
	proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
		Tool:   &dedov1.Tool{Title: "Get it", Description: "Returns it."},
		Errors: []*dedov1.ErrorReason{{Reason: "IT_NOT_FOUND", Code: code.Code_NOT_FOUND}},
	})
	fd.Service[0].Method[0].Options = opts
	return fd
}

// With notes=<file>, a noted RPC's tool description carries the note's
// paragraph before its errors; other tools are unchanged; repeated files
// add their paragraphs in order.
func TestPluginNotes(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	for path, content := range map[string]string{
		first:  `{"methods": {"acme.v1.AlphaService.GetAlphaService": {"description": "Only for pro accounts.", "extensions": {"x-plans": ["pro"]}}}}`,
		second: `{"methods": {"acme.v1.AlphaService.GetAlphaService": {"description": "Audited."}, "acme.v1.Missing.Get": {"description": "Ignored."}}}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp, stderr, err := execPlugin(t, "paths=source_relative,notes="+first+",notes="+second,
		documented("acme/v1/a.proto", "AlphaService"), documented("acme/v1/b.proto", "BetaService"))
	if err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr)
	}
	files := map[string]string{}
	for _, f := range resp.File {
		files[f.GetName()] = f.GetContent()
	}
	noted := strconv.Quote("Returns it.\n\nOnly for pro accounts.\n\nAudited.\n\nErrors: IT_NOT_FOUND (NOT_FOUND).")
	if !strings.Contains(files["acme/v1/a.mcp.pb.go"], "Description: "+noted) {
		t.Errorf("a.mcp.pb.go lacks Description: %s:\n%s", noted, files["acme/v1/a.mcp.pb.go"])
	}
	plain := strconv.Quote("Returns it.\n\nErrors: IT_NOT_FOUND (NOT_FOUND).")
	if !strings.Contains(files["acme/v1/b.mcp.pb.go"], "Description: "+plain) {
		t.Errorf("b.mcp.pb.go's tool changed without a note:\n%s", files["acme/v1/b.mcp.pb.go"])
	}
	for name, src := range files {
		if strings.Contains(src, "Ignored") || strings.Contains(src, "x-plans") {
			t.Errorf("%s carries what it should not:\n%s", name, src)
		}
	}
}

// A notes file that cannot be read fails the run, naming it.
func TestPluginNotesUnreadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	_, stderr, err := execPlugin(t, "notes="+missing, documented("acme/v1/a.proto", "AlphaService"))
	if err == nil || !strings.Contains(stderr, "missing.json") {
		t.Errorf("err = %v, stderr %q; want a failure naming missing.json", err, stderr)
	}
}
