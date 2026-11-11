package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

// sdkReplaces returns the replaces of the MCP Go SDK in a repository's go.mod.
func sdkReplaces(t *testing.T, dir string) []*modfile.Replace {
	t.Helper()
	f, err := modfile.Parse("go.mod", []byte(read(t, dir, "go.mod")), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []*modfile.Replace
	for _, r := range f.Replace {
		if r.Old.Path == sdkModule {
			out = append(out, r)
		}
	}
	return out
}

// A service builds against dedo's build of the MCP Go SDK, for the SDK
// release dedo is built with only, with a comment that says why.
func TestInitReplacesSDK(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/shop\n\ngo 1.26.1\n"})
	initOffline(t, dir)
	got := sdkReplaces(t, dir)
	if len(got) != 1 || got[0].Old.Version != sdkRelease || got[0].New.Path != sdkFork || got[0].New.Version != sdkForkRelease {
		t.Fatalf("replaces = %+v", got)
	}
	if !strings.Contains(read(t, dir, "go.mod"), sdkForkComment) {
		t.Errorf("go.mod lacks the comment:\n%s", read(t, dir, "go.mod"))
	}
	before := read(t, dir, "go.mod")
	initOffline(t, dir)
	if read(t, dir, "go.mod") != before {
		t.Errorf("a second dedo init changed go.mod:\n%s", read(t, dir, "go.mod"))
	}
}

// dedo upgrade moves the replace of an earlier dedo release to this one's,
// and keeps the service's other replaces.
func TestInitMovesSDKReplace(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/shop\n\ngo 1.26.1\n\n" +
		"replace github.com/modelcontextprotocol/go-sdk v1.7.0 => github.com/raveesh-me/go-sdk v1.7.0-dedo.1\n\n" +
		"replace example.com/lib => ../lib\n"})
	out := initOffline(t, dir)
	got := sdkReplaces(t, dir)
	if len(got) != 1 || got[0].New.Version != sdkForkRelease {
		t.Fatalf("replaces = %+v", got)
	}
	if !strings.Contains(read(t, dir, "go.mod"), "example.com/lib => ../lib") {
		t.Errorf("the service's own replace is gone:\n%s", read(t, dir, "go.mod"))
	}
	if !strings.Contains(out, "removed the replace of "+sdkModule+" by "+sdkFork+"@v1.7.0-dedo.1") {
		t.Errorf("output: %s", out)
	}
}

// A replace of the SDK that dedo did not write stays as it is.
func TestInitKeepsOwnSDKReplace(t *testing.T) {
	const mod = "module example.com/shop\n\ngo 1.26.1\n\nreplace github.com/modelcontextprotocol/go-sdk => ../go-sdk\n"
	dir := repo(t, map[string]string{"go.mod": mod})
	out := initOffline(t, dir)
	if got := sdkReplaces(t, dir); len(got) != 1 || got[0].New.Path != "../go-sdk" {
		t.Errorf("replaces = %+v", got)
	}
	if !strings.Contains(out, "keeps your replace of "+sdkModule) {
		t.Errorf("output: %s", out)
	}
}

// A BFF links the backend's generated MCP code, and Go reads replaces only
// from the main module's go.mod, so dedo init -bff gives bff/go.mod the
// replace too, beside its replace of the backend.
func TestInitBFFReplacesSDK(t *testing.T) {
	dir := bffRepo(t, nil)
	initBFFOffline(t, dir)
	got := sdkReplaces(t, filepath.Join(dir, bffDir))
	if len(got) != 1 || got[0].Old.Version != sdkRelease || got[0].New.Path != sdkFork || got[0].New.Version != sdkForkRelease {
		t.Fatalf("bff/go.mod replaces = %+v", got)
	}
	contains(t, "bff/go.mod", read(t, dir, "bff/go.mod"), sdkForkComment, "replace example.com/shop => ../")
}

// dedo upgrade moves the replace in a BFF's go.mod along with the root's,
// keeps the BFF's replace of the backend, and says to tidy the BFF.
func TestUpgradeMovesBFFSDKReplace(t *testing.T) {
	dir := bffRepo(t, map[string]string{
		"buf.yaml": "version: v2\nmodules:\n  - path: proto\n  - path: bff/proto\ndeps:\n  - buf.build/r-a/dedo:v0.30.0\n",
		"bff/go.mod": "module example.com/shop/bff\n\ngo 1.26.1\n\nreplace example.com/shop => ../\n\n" +
			"replace github.com/modelcontextprotocol/go-sdk v1.7.0 => github.com/raveesh-me/go-sdk v1.7.0-dedo.1\n",
	})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatalf("dedo upgrade: %v\n%s", err, out.String())
	}
	got := sdkReplaces(t, filepath.Join(dir, bffDir))
	if len(got) != 1 || got[0].Old.Version != sdkRelease || got[0].New.Version != sdkForkRelease {
		t.Fatalf("bff/go.mod replaces = %+v", got)
	}
	contains(t, "bff/go.mod", read(t, dir, "bff/go.mod"), sdkForkComment, "replace example.com/shop => ../")
	contains(t, "output", out.String(),
		"bff/go.mod: removed the replace of "+sdkModule+" by "+sdkFork+"@v1.7.0-dedo.1",
		"bff/go.mod: replaces "+sdkModule, "run go mod tidy in bff/")
}

// -wasm refuses a lint plugin release that cannot read built-in house
// rules, since go build -X would leave them out silently.
func TestWasmNeedsEmbedSupport(t *testing.T) {
	for release, ok := range map[string]bool{"v0.14.0": false, "v0.15.0": true, "v0.16.1": true} {
		if err := checkEmbedSupport(release); (err == nil) != ok {
			t.Errorf("%s: err = %v", release, err)
		}
	}
}
