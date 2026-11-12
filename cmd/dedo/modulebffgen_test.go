package main

import "testing"

// Which inputs of the backend's buf.gen.yaml run its generators over a
// BFF's protos, as buf generate reads them: a directory or proto file holds
// a proto at or below it, and paths and exclude_paths filter only by
// entries inside the proto's module, workspace-relative. Each case is what
// buf 1.65 generates for that buf.gen.yaml in a workspace with the modules
// proto and bff/proto.
func TestRootGenerates(t *testing.T) {
	const (
		mod   = "bff/proto"
		web   = "bff/proto/shop/web/v1/web.proto"
		admin = "bff/proto/shop/admin/v1/admin.proto"
		head  = "version: v2\nplugins:\n  - local: protoc-gen-go\n    out: gen/go\n"
	)
	whole := "has no inputs, which means the whole workspace, bff/proto included"
	for _, tt := range []struct {
		name, inputs string
		files        []string
		want         string
	}{
		{name: "no inputs", files: []string{web}, want: whole},
		{name: "an empty list", inputs: "inputs: []\n", files: []string{web}, want: whole},
		{name: "a v1 template", inputs: "", files: []string{web}, want: whole},
		{name: "no files", files: nil},
		{name: "the repository", inputs: "inputs:\n  - directory: .\n", files: []string{web}, want: "its input . holds " + web},
		{name: "the repository, spelled ./", inputs: "inputs:\n  - directory: ./\n", files: []string{web}, want: "its input ./ holds " + web},
		{name: "the proto root", inputs: "inputs:\n  - directory: proto\n", files: []string{web}},
		{name: "the proto root, spelled ./proto/", inputs: "inputs:\n  - directory: ./proto/\n", files: []string{web}},
		{name: "the BFF's directory", inputs: "inputs:\n  - directory: bff\n", files: []string{web}, want: "its input bff holds " + web},
		{name: "the BFF's module too", inputs: "inputs:\n  - directory: proto\n  - directory: bff/proto\n", files: []string{web}, want: "its input bff/proto holds " + web},
		{name: "a directory beside the module", inputs: "inputs:\n  - directory: bff/other\n", files: []string{web}},
		{name: "one of its files", inputs: "inputs:\n  - proto_file: bff/proto/shop/web/v1/web.proto\n", files: []string{web}, want: "its input bff/proto/shop/web/v1/web.proto holds " + web},
		{name: "another of its files", inputs: "inputs:\n  - proto_file: " + admin + "\n", files: []string{web}},
		{name: "a remote module", inputs: "inputs:\n  - module: buf.build/acme/shop\n", files: []string{web}},
		{name: "a git repository", inputs: "inputs:\n  - git_repo: https://github.com/acme/shop\n", files: []string{web}},
		// exclude_paths and paths name files and directories inside a
		// module; buf ignores one that holds a module, such as bff.
		{name: "excluded by a directory in the module", inputs: "inputs:\n  - directory: .\n    exclude_paths: [bff/proto/shop]\n", files: []string{web, admin}},
		{name: "one file of two excluded", inputs: "inputs:\n  - directory: .\n    exclude_paths: [bff/proto/shop/web]\n", files: []string{web, admin}, want: "its input . holds " + admin},
		{name: "excluded by the directory holding the module", inputs: "inputs:\n  - directory: .\n    exclude_paths: [bff]\n", files: []string{web}, want: "its input . holds " + web},
		{name: "excluded by a path relative to the module", inputs: "inputs:\n  - directory: .\n    exclude_paths: [shop/web]\n", files: []string{web}, want: "its input . holds " + web},
		{name: "filtered to the backend", inputs: "inputs:\n  - directory: .\n    paths: [proto/shop]\n", files: []string{web}},
		{name: "filtered to the BFF", inputs: "inputs:\n  - directory: .\n    paths: [bff/proto/shop/web]\n", files: []string{web, admin}, want: "its input . holds " + web},
		{name: "filtered to the directory holding the module", inputs: "inputs:\n  - directory: .\n    paths: [bff]\n", files: []string{web}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := head + tt.inputs
			if tt.name == "a v1 template" {
				src = "version: v1\nplugins:\n  - plugin: go\n    out: gen/go\n"
			}
			doc, err := parseYAML([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if got := rootGenerates(doc, mod, tt.files); got != tt.want {
				t.Errorf("rootGenerates() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The finding names the backend's first buf module as the input to keep:
// the first outside a nested Go module, or proto when there is none.
func TestBackendModule(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mods   []bufModule
		nested []string
		want   string
	}{
		{name: "after the BFF's", mods: []bufModule{{Path: "bff/proto"}, {Path: "api"}}, nested: []string{"bff"}, want: "api"},
		{name: "the first of several", mods: []bufModule{{Path: "proto"}, {Path: "third_party/vendored"}}, nested: []string{"bff"}, want: "proto"},
		{name: "only the BFF's", mods: []bufModule{{Path: "bff/proto"}}, nested: []string{"bff"}, want: "proto"},
		{name: "none", want: "proto"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := backendModule(tt.mods, tt.nested); got != tt.want {
				t.Errorf("backendModule() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Only an external service needs the backend's buf.gen.yaml read, and a
// buf.gen.yaml dedo cannot read fails dedo modules rather than passing it.
func TestBFFGenFinding(t *testing.T) {
	external := []moduleService{{File: "bff/proto/shop/web/v1/web.proto", Name: "WebService", Audience: "AUDIENCE_EXTERNAL_PUBLIC"}}
	internal := []moduleService{{File: "bff/proto/shop/web/v1/web.proto", Name: "WebService", Audience: bffAudience}}
	for _, tt := range []struct {
		name     string
		gen      string // "" for no buf.gen.yaml
		services []moduleService
		finding  bool
		err      bool
	}{
		{name: "no buf.gen.yaml", services: external},
		{name: "internal services, unreadable buf.gen.yaml", gen: "- not a mapping\n", services: internal},
		{name: "external services, unreadable buf.gen.yaml", gen: "- not a mapping\n", services: external, err: true},
		{name: "external services, no inputs", gen: "version: v2\n", services: external, finding: true},
		{name: "external services, the backend's inputs", gen: "version: v2\ninputs:\n  - directory: proto\n", services: external},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{}
			if tt.gen != "" {
				files["buf.gen.yaml"] = tt.gen
			}
			dir := repo(t, files)
			got, err := bffGenFinding(dir, "bff/proto", tt.services, "proto")
			if (err != nil) != tt.err {
				t.Fatalf("err = %v, want error %v", err, tt.err)
			}
			if (got != "") != tt.finding {
				t.Errorf("finding = %q, want one %v", got, tt.finding)
			}
		})
	}
}
