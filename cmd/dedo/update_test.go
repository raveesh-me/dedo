package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func stubNetwork(t *testing.T, latest string) *string {
	t.Helper()
	l, i := latestRelease, goInstall
	t.Cleanup(func() { latestRelease, goInstall = l, i })
	latestRelease = func() (string, error) { return latest, nil }
	installed := new(string)
	goInstall = func(release string, _ io.Writer) error {
		*installed = release
		return nil
	}
	return installed
}

func TestUpdateInstallsLatest(t *testing.T) {
	installed := stubNetwork(t, "v9.9.9")
	var out bytes.Buffer
	if err := run([]string{"update"}, &out); err != nil {
		t.Fatal(err)
	}
	if *installed != "v9.9.9" || !strings.Contains(out.String(), "run dedo upgrade") {
		t.Errorf("installed %q:\n%s", *installed, out.String())
	}
}

func TestUpdateWhenCurrent(t *testing.T) {
	installed := stubNetwork(t, version)
	var out bytes.Buffer
	if err := run([]string{"update"}, &out); err != nil {
		t.Fatal(err)
	}
	if *installed != "" || !strings.Contains(out.String(), "is installed") {
		t.Errorf("installed %q:\n%s", *installed, out.String())
	}
}

// upgrade pins the options module to this release and always refreshes
// buf.lock and the generated code.
func TestUpgradeMovesEverythingTogether(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":   "module example.com/notes\n",
		"buf.lock": "version: v2\n",
		"buf.yaml": "version: v2\ndeps:\n  - buf.build/googleapis/googleapis\n  - buf.build/bufbuild/protovalidate\n  - buf.build/r-a/dedo:v0.1.0\nplugins:\n  - plugin: .tools/buf-plugin-dedo-lint\n",
	})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatalf("dedo upgrade: %v\n%s", err, out.String())
	}
	if got := read(t, dir, "buf.yaml"); !strings.Contains(got, "buf.build/r-a/dedo:"+version) || strings.Contains(got, "v0.1.0") {
		t.Errorf("options module not moved to %s:\n%s", version, got)
	}
	tail := out.String()[strings.LastIndex(out.String(), "fetching"):]
	for _, want := range []string{"buf dep update", "buf generate", "go mod tidy", "upgraded to dedo " + version} {
		if !strings.Contains(tail, want) {
			t.Errorf("upgrade lacks %q:\n%s", want, tail)
		}
	}
}

func TestNewerHint(t *testing.T) {
	stubNetwork(t, "v99.0.0")
	var out bytes.Buffer
	newerHint(&out)
	if !strings.HasPrefix(out.String(), "dedo v99.0.0 is out") {
		t.Errorf("hint = %q", out.String())
	}
	stubNetwork(t, version)
	out.Reset()
	newerHint(&out)
	if out.Len() != 0 {
		t.Errorf("hint for the current release: %q", out.String())
	}
}

func TestUpgradeNeedsGoModule(t *testing.T) {
	if err := run([]string{"upgrade", "-offline", "-dir", t.TempDir()}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "dedo init") {
		t.Errorf("err = %v", err)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.11.0", "v0.10.0", true}, {"v0.10.0", "v0.10.0", false}, {"v0.9.9", "v0.10.0", false}, {"v1.0.0", "v0.99.0", true}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
