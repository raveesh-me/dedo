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

// A repository on connect-go v1 is migrated by connect-go's own tool, once
// before regenerating and once after.
func TestUpgradeMigratesConnectV1(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":       "module example.com/notes\n\nrequire connectrpc.com/connect v1.19.1\n",
		"buf.lock":     "version: v2\n",
		"buf.gen.yaml": "version: v2\nplugins: []\n",
	})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	migrate := strings.Join(migrateCmd, " ")
	if strings.Count(out.String(), migrate) != 2 {
		t.Errorf("want the migration tool twice:\n%s", out.String())
	}
	if i, j := strings.LastIndex(out.String(), "buf generate"), strings.LastIndex(out.String(), migrate); i < 0 || j < i {
		t.Errorf("the second pass must follow buf generate:\n%s", out.String())
	}
}

func TestUpgradeLeavesConnectV2Alone(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/notes\n\nrequire connectrpc.com/connect/v2 v2.0.1\n", "buf.lock": "version: v2\n"})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "connect-go-v2-migrate") {
		t.Error("migration tool run on a v2 repository")
	}
}

// An upgrade re-pins the generators dedo set up, and replaces the
// connect-go v1 generator earlier releases pinned under the same name.
func TestUpgradeRepinsGenerators(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":       "module example.com/notes\n\ntool (\n\tconnectrpc.com/connect/cmd/protoc-gen-connect-go\n\tgithub.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp\n)\n",
		"buf.lock":     "version: v2\n",
		"buf.gen.yaml": "version: v2\nplugins:\n  - local: [\"go\", \"tool\", \"protoc-gen-go\"]\n    out: gen/go\n  - local: [\"go\", \"tool\", \"protoc-gen-connect-go\"]\n    out: gen/go\n",
	})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"go get -tool connectrpc.com/connect/cmd/protoc-gen-connect-go@none",
		"go get -tool connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@" + connectRelease,
		"go get -tool google.golang.org/protobuf/cmd/protoc-gen-go",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("upgrade lacks %q:\n%s", want, out.String())
		}
	}
	if i, j := strings.Index(out.String(), "protoc-gen-connect-go@none"), strings.Index(out.String(), "v2/cmd/protoc-gen-connect-go@"); j < i {
		t.Error("the v1 generator must be removed before the v2 one is pinned")
	}
}
