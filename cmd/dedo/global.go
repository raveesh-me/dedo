package main

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// A repository without go.mod has no Go tool to pin the lint plugin with,
// so dedo installs it once per machine, into $GOBIN like dedo itself, and
// vendors a copy into the repository's .tools/. buf.yaml lists that copy in
// every repository, and the options module's label in buf.yaml says which
// release it must be.

// Seams for tests: installing needs the network, and the version is read
// from a Go binary.
var (
	installPlugin = func(release string, out io.Writer) error {
		cmd := exec.Command("go", "install", lintPluginPkg+"@"+release)
		cmd.Stdout, cmd.Stderr = out, out
		return cmd.Run()
	}
	pluginVersion = func(path string) (string, error) {
		info, err := buildinfo.ReadFile(path)
		if err != nil {
			return "", err
		}
		return info.Main.Version, nil
	}
)

// goBin is where go install puts a command: $GOBIN, else $GOPATH/bin.
func goBin(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if bin, _ := exec.Command("go", "env", "GOBIN").Output(); strings.TrimSpace(string(bin)) != "" {
		return filepath.Join(strings.TrimSpace(string(bin)), name)
	}
	gopath, _ := exec.Command("go", "env", "GOPATH").Output()
	return filepath.Join(strings.TrimSpace(string(gopath)), "bin", name)
}

// globalPlugin is where dedo install puts the lint plugin.
func globalPlugin() string { return goBin("buf-plugin-dedo-lint") }

// install installs the lint plugin at release into $GOBIN: dedo install.
func install(release string, out io.Writer) error {
	fmt.Fprintf(out, "$ go install %s@%s\n", lintPluginPkg, release)
	if err := installPlugin(release, out); err != nil {
		return fmt.Errorf("installing the lint plugin %s: %w", release, err)
	}
	fmt.Fprintf(out, "installed buf-plugin-dedo-lint %s at %s\n", release, globalPlugin())
	return nil
}

// vendorPlugin copies the global lint plugin into dir's .tools/, first
// installing it when it is missing or at another release than release.
func vendorPlugin(dir, release string, out io.Writer) error {
	vendored := filepath.Join(dir, pluginPath())
	if v, err := pluginVersion(vendored); err == nil && v == release {
		fmt.Fprintf(out, "%s is at %s\n", pluginPath(), release)
		return nil
	}
	global := globalPlugin()
	if v, err := pluginVersion(global); err != nil || v != release {
		if err := install(release, out); err != nil {
			return err
		}
	}
	if err := copyExecutable(global, vendored); err != nil {
		return fmt.Errorf("vendoring the lint plugin: %w", err)
	}
	fmt.Fprintf(out, "vendored %s %s into %s\n", global, release, pluginPath())
	return nil
}

// copyExecutable copies src to dst through a temporary file, so a running
// lint never sees half a binary.
func copyExecutable(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// pinnedRelease is the options module's label in dir's buf.yaml, the
// release the repository's lint plugin must be; this dedo's when the label
// is missing.
func pinnedRelease(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "buf.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no buf.yaml in %s: run dedo init first", dir)
	}
	if err != nil {
		return "", err
	}
	if m := optionsPin.Find(b); m != nil {
		return strings.TrimPrefix(string(m), optionsModule+":"), nil
	}
	return version, nil
}

// uninstall deletes dedo and the lint plugin from $GOBIN: dedo uninstall. It
// changes no repository; each keeps its own copy in .tools/, ignored by git.
func uninstall(out io.Writer) error {
	removed := 0
	for _, p := range []string{globalPlugin(), installedCLI()} {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("removing %s: %w", p, err)
		}
		fmt.Fprintf(out, "removed %s\n", p)
		removed++
	}
	if removed == 0 {
		fmt.Fprintf(out, "nothing to remove: neither %s nor %s exists\n", globalPlugin(), installedCLI())
		return nil
	}
	fmt.Fprintln(out, "repositories keep their copy in .tools/; rm -rf .tools/ in each to remove it")
	return nil
}
