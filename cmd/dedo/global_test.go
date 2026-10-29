package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubGlobalPlugin points GOBIN at a new directory, and has go install
// write a fake plugin there: a script that prints its release, which
// pluginVersion reads back. It returns the releases installed.
func stubGlobalPlugin(t *testing.T) *[]string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake plugin is a shell script")
	}
	t.Setenv("GOBIN", t.TempDir())
	i, v := installPlugin, pluginVersion
	t.Cleanup(func() { installPlugin, pluginVersion = i, v })
	var installed []string
	installPlugin = func(release string, _ io.Writer) error {
		installed = append(installed, release)
		return writeFakePlugin(globalPlugin(), release)
	}
	pluginVersion = func(path string) (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, release, _ := strings.Cut(strings.Split(string(b), "\n")[1], "# ")
		return release, nil
	}
	return &installed
}

// writeFakePlugin writes a plugin that answers -dedo-rules.
func writeFakePlugin(path, release string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("#!/bin/sh\n# "+release+"\necho ok\n"), 0o755)
}

func TestInstallCommand(t *testing.T) {
	installed := stubGlobalPlugin(t)
	var out bytes.Buffer
	if err := run([]string{"install", "-version", "v0.14.0"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(*installed) != 1 || (*installed)[0] != "v0.14.0" || !strings.Contains(out.String(), globalPlugin()) {
		t.Errorf("installed %v:\n%s", *installed, out.String())
	}
}

// Vendoring installs the global plugin only when it is missing or at
// another release, and copies it only when the vendored one differs.
func TestVendorPlugin(t *testing.T) {
	installed := stubGlobalPlugin(t)
	dir := t.TempDir()
	vendored := filepath.Join(dir, pluginPath())
	for _, c := range []struct {
		release string
		install []string
	}{
		{"v0.15.0", []string{"v0.15.0"}},            // no global plugin yet
		{"v0.15.0", []string{"v0.15.0"}},            // in place: nothing to do
		{"v0.14.0", []string{"v0.15.0", "v0.14.0"}}, // another repository's release
	} {
		if err := vendorPlugin(dir, c.release, io.Discard); err != nil {
			t.Fatal(err)
		}
		if strings.Join(*installed, " ") != strings.Join(c.install, " ") {
			t.Errorf("%s: installed %v, want %v", c.release, *installed, c.install)
		}
		if got, _ := pluginVersion(vendored); got != c.release {
			t.Errorf("%s: vendored %q", c.release, got)
		}
	}
}

// dedo tools vendors the plugin at the options module's release in
// buf.yaml and checks the rules with it, go.mod or not.
func TestToolsVendorsPlugin(t *testing.T) {
	stubGlobalPlugin(t)
	for _, gomod := range []bool{false, true} {
		files := map[string]string{
			"buf.yaml":  "version: v2\ndeps:\n  - buf.build/r-a/dedo:v0.14.0\n",
			"rules.yml": "use: [DEDO]\n",
		}
		if gomod {
			files["go.mod"] = "module example.com/shop\n"
		}
		dir := repo(t, files)
		var out bytes.Buffer
		if err := run([]string{"tools", "-dir", dir}, &out); err != nil {
			t.Fatalf("go.mod %v: dedo tools: %v\n%s", gomod, err, out.String())
		}
		if v, _ := pluginVersion(filepath.Join(dir, pluginPath())); v != "v0.14.0" || !strings.Contains(out.String(), "lint rules: ok") {
			t.Errorf("go.mod %v: vendored %q:\n%s", gomod, v, out.String())
		}
	}
}

// dedo upgrade -lint moves the options module's label, which names the lint
// plugin's release.
func TestUpgradeLintMovesOptionsPin(t *testing.T) {
	dir := repo(t, map[string]string{"buf.yaml": "version: v2\ndeps:\n  - buf.build/r-a/dedo:v0.14.0\n"})
	var out bytes.Buffer
	if err := run([]string{"upgrade", "-lint", "-offline", "-dir", dir}, &out); err != nil {
		t.Fatalf("dedo upgrade -lint: %v\n%s", err, out.String())
	}
	if got := read(t, dir, "buf.yaml"); !strings.Contains(got, optionsModule+":"+version) {
		t.Errorf("buf.yaml:\n%s", got)
	}
	for _, want := range []string{"buf dep update", "dedo tools"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestToolsNeedsBufYAML(t *testing.T) {
	stubGlobalPlugin(t)
	if err := run([]string{"tools", "-dir", t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), "dedo init") {
		t.Errorf("err = %v", err)
	}
}
