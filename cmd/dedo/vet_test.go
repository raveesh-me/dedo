package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain lets go vet run this test binary as its -vettool, as it runs
// the dedo command.
func TestMain(m *testing.M) {
	if isVetTool(os.Args[1:]) {
		vetTool()
	}
	os.Exit(m.Run())
}

func TestIsVetTool(t *testing.T) {
	for args, want := range map[string]bool{
		"":                       false,
		"vet":                    false,
		"-V=full":                true,
		"-flags":                 true,
		"-json /tmp/b/vet.cfg":   true,
		"init -dir x":            false,
		"templates Makefile.api": false,
		"templates x.cfg":        false,
		"/tmp/b/vet.cfg":         true,
	} {
		if got := isVetTool(strings.Fields(args)); got != want {
			t.Errorf("isVetTool(%q) = %v", args, got)
		}
	}
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// vetRepo is a service with a backend for frontend, whose dedo.yaml
// forbids a package that one file of each imports, and allows it in
// internal/legacy.
var vetRepo = map[string]string{
	"buf.yaml":  "version: v2\nmodules:\n  - path: proto\n  - path: bff/proto\n",
	"dedo.yaml": "go:\n  rules:\n    - id: HOUSE_GO_NO_HTTPUTIL\n      forbid_import: net/http/httputil\n      allow_in: [internal/legacy/**]\n      message: Proxy through the gateway.\n      doc: https://example.com/rules#httputil\n",
	"go.mod":    "module example.com/svc\n\ngo 1.26\n",
	"internal/orders/orders.go": `package orders

import _ "net/http/httputil"

//dedo:ignore DEDO_GO_SERVER_INTERCEPTORS
var x = 1
`,
	"internal/legacy/legacy.go": "package legacy\n\nimport _ \"net/http/httputil\"\n",
	"internal/clean/clean.go": `package clean

import (
	//dedo:ignore HOUSE_GO_NO_HTTPUTIL the gateway cannot proxy uploads yet
	_ "net/http/httputil"
)
`,
	"bff/go.mod":  "module example.com/svc/bff\n\ngo 1.26\n",
	"bff/bff.go":  "package bff\n\nimport _ \"net/http/httputil\"\n",
	"proto/x.txt": "",
	"bff/proto/x": "",
}

func TestVet(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	dir := t.TempDir()
	writeTree(t, dir, vetRepo)
	t.Setenv("GOWORK", "off")
	var out strings.Builder
	err := vet(dir, &out)
	if err == nil || !strings.Contains(err.Error(), "vet: 3 finding(s)") {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"internal/orders/orders.go:3:8: HOUSE_GO_NO_HTTPUTIL: internal/orders/orders.go imports net/http/httputil: Proxy through the gateway. (https://example.com/rules#httputil)",
		"internal/orders/orders.go:5:1: DEDO_GO_IGNORE_REASON: //dedo:ignore DEDO_GO_SERVER_INTERCEPTORS gives no reason",
		"bff/bff.go:3:8: HOUSE_GO_NO_HTTPUTIL: bff/bff.go imports net/http/httputil",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "legacy") || strings.Contains(got, "clean") {
		t.Errorf("allowed or suppressed imports reported:\n%s", got)
	}
}

func TestVetClean(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"go.mod": "module example.com/clean\n\ngo 1.26\n"})
	t.Setenv("GOWORK", "off")
	if err := vet(dir, &strings.Builder{}); err != nil {
		t.Fatalf("a module without packages: %v", err)
	}
	writeTree(t, dir, map[string]string{"a/a.go": "package a\n"})
	var out strings.Builder
	if err := vet(dir, &out); err != nil {
		t.Fatalf("vet: %v\n%s", err, out.String())
	}
	if got := out.String(); got != "dedo vet: no findings (4 rules)\n" {
		t.Errorf("output = %q", got)
	}
}

func TestVetRejectsBadRules(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"buf.yaml":  "version: v2\n",
		"dedo.yaml": "go:\n  disable: [DEDO_GO_IGNORE_REASON]\n",
		"go.mod":    "module example.com/x\n",
	})
	if err := vet(dir, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "is final") {
		t.Errorf("err = %v", err)
	}
	writeTree(t, dir, map[string]string{"dedo.yaml": "go:\n  rules:\n    - id: NOPE\n"})
	if err := vet(dir, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "dedo.yaml") {
		t.Errorf("err = %v", err)
	}
}

// A package that does not compile fails the run, with go vet's error.
func TestVetBuildError(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"go.mod": "module example.com/broken\n\ngo 1.26\n", "a/a.go": "package a\n\nvar x int = \"no\"\n"})
	t.Setenv("GOWORK", "off")
	if err := vet(dir, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "go vet") {
		t.Errorf("err = %v", err)
	}
}

func TestToolsListsGoRules(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, vetRepo)
	var out strings.Builder
	if err := goRules(dir, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"go: 5 rules, 1 of them house rules; 5 run",
		"HOUSE_GO_NO_HTTPUTIL (" + filepath.Join(dir, "dedo.yaml") + ") forbids importing net/http/httputil",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}
