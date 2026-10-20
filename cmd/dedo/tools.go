package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// lintPluginPkg is the lint plugin's command, pinned in a service's go.mod
// as a Go tool.
const lintPluginPkg = "github.com/raveesh-me/dedo/lint/cmd/buf-plugin-dedo-lint"

// pluginPath is where buf.yaml finds the plugin. On Windows the file needs
// .exe; buf.yaml still names it without, and Windows finds it by extension.
func pluginPath() string {
	p := filepath.Join(".tools", "buf-plugin-dedo-lint")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	return p
}

func buildPluginCmd() []string {
	return []string{"go", "build", "-o", pluginPath(), lintPluginPkg}
}

// tools builds the lint plugin pinned in go.mod into .tools/, for a fresh
// clone, a CI job, or after the pin changes. It is what make tools does,
// without make.
func tools(dir string, out io.Writer) error {
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no go.mod in %s: run dedo init first", dir)
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(gomod), lintPluginPkg) {
		return fmt.Errorf("go.mod does not pin %s: run dedo init, or go get -tool %s@%s", lintPluginPkg, lintPluginPkg, version)
	}
	c := buildPluginCmd()
	fmt.Fprintf(out, "$ %s\n", strings.Join(c, " "))
	cmd := exec.Command(c[0], c[1:]...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the lint plugin: %w", err)
	}
	fmt.Fprintf(out, "built %s\n", pluginPath())
	return nil
}
