package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo makes dir a git repository whose branch main holds files, then
// checks out a branch of its own, as a pull request does.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf is not installed")
	}
	dir := repo(t, files)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "base"},
		{"checkout", "-q", "-b", "change"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

const breakingBufYAML = `version: v2
modules:
  - path: proto
  - path: lib/proto
  - path: bff/proto
    breaking:
      ignore:
        - bff/proto
breaking:
  use:
    - FILE
`

// breakingRepo is a workspace of three modules on main: the API, a library
// and a BFF's, which its own breaking section ignores.
func breakingRepo(t *testing.T) string {
	return gitRepo(t, map[string]string{
		"buf.yaml":                      breakingBufYAML,
		"proto/shop/a/v1/a.proto":       "syntax = \"proto3\";\npackage shop.a.v1;\nmessage A {\n  string x = 1;\n  string y = 2;\n}\n",
		"lib/proto/lib/v1/l.proto":      "syntax = \"proto3\";\npackage lib.v1;\nmessage L {\n  string x = 1;\n}\n",
		"bff/proto/shop/web/v1/w.proto": "syntax = \"proto3\";\npackage shop.web.v1;\nmessage W {\n  string x = 1;\n}\n",
	})
}

func runBreaking(dir, base string) (string, error) {
	var out bytes.Buffer
	err := run([]string{"breaking", "-dir", dir, "-against", base}, &out)
	return out.String(), err
}

// A module new on the branch, such as the BFF's dedo init -bff adds, with
// its own breaking section and an import of the API module, has nothing to
// break: it is skipped, and the others are each compared with their own
// directory on the base branch. buf breaking over the whole workspace
// fails on it instead ("input contained 2 images, whereas against
// contained 1").
func TestBreakingSkipsNewModules(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"buf.yaml":                 "version: v2\nmodules:\n  - path: proto\n  - path: lib/proto\nbreaking:\n  use:\n    - FILE\n",
		"proto/shop/a/v1/a.proto":  "syntax = \"proto3\";\npackage shop.a.v1;\nmessage A {\n  string x = 1;\n}\n",
		"lib/proto/lib/v1/l.proto": "syntax = \"proto3\";\npackage lib.v1;\nmessage L {\n  string x = 1;\n}\n",
	})
	writeFile(t, dir, "buf.yaml", breakingBufYAML)
	writeFile(t, dir, "bff/proto/shop/web/v1/w.proto", "syntax = \"proto3\";\npackage shop.web.v1;\nimport \"shop/a/v1/a.proto\";\nmessage W {\n  shop.a.v1.A a = 1;\n}\n")

	whole := exec.Command("buf", "breaking", "--against", ".git#branch=main")
	whole.Dir = dir
	if b, err := whole.CombinedOutput(); err == nil {
		t.Logf("buf breaking over the whole workspace passed too:\n%s", b)
	}

	out, err := runBreaking(dir, "main")
	if err != nil {
		t.Fatalf("dedo breaking: %v\n%s", err, out)
	}
	contains(t, "output", out,
		"buf breaking proto --against .git#branch=main,subdir=proto\n",
		"buf breaking lib/proto --against .git#branch=main,subdir=lib/proto\n",
		"buf breaking bff/proto: skipped; main has no .proto files in bff/proto, so nothing to break",
	)
}

// A breaking change fails the run and names its module; a module whose
// own breaking section ignores it, a BFF's, never fails.
func TestBreakingFailsPerModule(t *testing.T) {
	dir := breakingRepo(t)
	writeFile(t, dir, "proto/shop/a/v1/a.proto", "syntax = \"proto3\";\npackage shop.a.v1;\nmessage A {\n  string x = 1;\n}\n")
	writeFile(t, dir, "bff/proto/shop/web/v1/w.proto", "syntax = \"proto3\";\npackage shop.web.v1;\nmessage W {}\n")
	out, err := runBreaking(dir, "main")
	if err == nil || !strings.Contains(err.Error(), "proto") || strings.Contains(err.Error(), "lib/proto") || strings.Contains(err.Error(), "bff/proto") {
		t.Fatalf("err = %v, want a failure in proto only\n%s", err, out)
	}
	contains(t, "output", out, `Previously present field "2" with name "y" on message "A" was deleted.`)
}

// Without the base branch, or before it has protos, there is nothing to
// compare with.
func TestBreakingWithoutBase(t *testing.T) {
	dir := breakingRepo(t)
	out, err := runBreaking(dir, "release")
	if err != nil || !strings.Contains(out, "buf breaking: skipped; there is no branch release to compare with (set API_BASE to compare with another branch)") {
		t.Errorf("err = %v, output:\n%s", err, out)
	}
	empty := gitRepo(t, map[string]string{"buf.yaml": breakingBufYAML, "readme.md": "x\n"})
	if err := os.MkdirAll(filepath.Join(empty, "proto"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, empty, "proto/shop/a/v1/a.proto", "syntax = \"proto3\";\npackage shop.a.v1;\nmessage A {}\n")
	out, err = runBreaking(empty, "main")
	if err != nil || strings.Count(out, ": skipped; main has no .proto files in") != 3 {
		t.Errorf("err = %v, output:\n%s", err, out)
	}
}

// A buf.yaml without modules is one module at the root.
func TestBreakingRootModule(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"buf.yaml":          "version: v2\nbreaking:\n  use:\n    - FILE\n",
		"shop/a/v1/a.proto": "syntax = \"proto3\";\npackage shop.a.v1;\nmessage A {\n  string x = 1;\n}\n",
	})
	out, err := runBreaking(dir, "main")
	if err != nil {
		t.Fatalf("dedo breaking: %v\n%s", err, out)
	}
	contains(t, "output", out, "buf breaking . --against .git#branch=main\n")
}
