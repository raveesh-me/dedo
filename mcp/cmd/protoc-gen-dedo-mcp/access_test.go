package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// documented is serviceFile with a tool description, a declared error and
// scopes on its RPC.
func documented(name, service string, scopes ...string) *descriptorpb.FileDescriptorProto {
	fd := serviceFile(name, service)
	opts := &descriptorpb.MethodOptions{}
	proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
		Scopes: scopes,
		Tool:   &dedov1.Tool{Title: "Get it", Description: "Returns it."},
		Errors: []*dedov1.ErrorReason{{Reason: "CONDITION_NOT_MET", Code: code.Code_PERMISSION_DENIED}},
	})
	fd.Service[0].Method[0].Options = opts
	return fd
}

// With access=<file>, a tool gets each of its scopes' paragraphs before its
// errors, in scope order and then file order, and their meta in its _meta;
// a tool whose scopes no file names is unchanged.
func TestPluginAccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	for path, content := range map[string]string{
		first: `{"scopes": {
			"taxes/tariff-codes.readonly": {
				"description": "Only for: Agencies on the Founders plan (` + "`founders`" + `). Other callers get 403 ` + "`PERMISSION_DENIED`" + ` with reason ` + "`CONDITION_NOT_MET`" + `.",
				"extensions": {"x-commerce-conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}]},
				"meta": {"conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}]}
			},
			"taxes/admin": {"description": "Admins only.", "meta": {"admin": true}},
			"taxes/unused": {"description": "Ignored."}
		}}`,
		second: `{"scopes": {"taxes/tariff-codes.readonly": {"description": "Audited."}}}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resp, stderr, err := execPlugin(t, "paths=source_relative,access="+first+",access="+second,
		documented("acme/v1/a.proto", "AlphaService", "taxes/tariff-codes.readonly", "taxes/admin"),
		documented("acme/v1/b.proto", "BetaService", "taxes/tax-codes.readonly"))
	if err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr)
	}
	files := map[string]string{}
	for _, f := range resp.File {
		files[f.GetName()] = f.GetContent()
	}
	a := files["acme/v1/a.mcp.pb.go"]
	description := strconv.Quote("Returns it.\n\n" +
		"Only for: Agencies on the Founders plan (`founders`). Other callers get 403 `PERMISSION_DENIED` with reason `CONDITION_NOT_MET`.\n\n" +
		"Audited.\n\nAdmins only.\n\n" +
		"Errors: CONDITION_NOT_MET (PERMISSION_DENIED).")
	if !strings.Contains(a, "Description: "+description) {
		t.Errorf("a.mcp.pb.go lacks Description: %s:\n%s", description, a)
	}
	meta := strconv.Quote(`{"admin":true,"conditions":[{"id":"founders","label":"Agencies on the Founders plan"}]}`)
	if !regexp.MustCompile(`Meta:\s+mcpserver\.Meta\(` + regexp.QuoteMeta(meta) + `\),`).MatchString(a) {
		t.Errorf("a.mcp.pb.go lacks Meta: mcpserver.Meta(%s):\n%s", meta, a)
	}
	b := files["acme/v1/b.mcp.pb.go"]
	if !strings.Contains(b, "Description: "+strconv.Quote("Returns it.\n\nErrors: CONDITION_NOT_MET (PERMISSION_DENIED).")) || strings.Contains(b, "Meta:") {
		t.Errorf("b.mcp.pb.go's tool changed without requirements:\n%s", b)
	}
	for name, src := range files {
		if strings.Contains(src, "Ignored") || strings.Contains(src, "x-commerce-conditions") {
			t.Errorf("%s carries what it should not:\n%s", name, src)
		}
	}
}

// One meta key that two of a tool's scopes give fails the run, as does a
// file that cannot be read.
func TestPluginAccessRejects(t *testing.T) {
	twice := filepath.Join(t.TempDir(), "twice.json")
	if err := os.WriteFile(twice, []byte(`{"scopes": {"s1": {"meta": {"plans": 1}}, "s2": {"meta": {"plans": 2}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, stderr, err := execPluginResponse(t, "access="+twice, documented("acme/v1/a.proto", "AlphaService", "s1", "s2"))
	if err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr)
	}
	if !strings.Contains(resp.GetError(), `meta "plans" twice`) {
		t.Errorf("error = %q, want meta plans twice", resp.GetError())
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	_, stderr, err = execPlugin(t, "access="+missing, documented("acme/v1/a.proto", "AlphaService", "s1"))
	if err == nil || !strings.Contains(stderr, "missing.json") {
		t.Errorf("err = %v, stderr %q; want a failure naming missing.json", err, stderr)
	}
}
