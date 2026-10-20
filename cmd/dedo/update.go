package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const cliPkg = "github.com/raveesh-me/dedo/cmd/dedo"

// Seams for tests: both need the network.
var (
	latestRelease = func() (string, error) {
		out, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", cliPkg+"@latest").Output()
		if err != nil {
			return "", fmt.Errorf("finding the latest dedo release: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	goInstall = func(release string, out io.Writer) error {
		cmd := exec.Command("go", "install", cliPkg+"@"+release)
		cmd.Stdout, cmd.Stderr = out, out
		return cmd.Run()
	}
)

// update installs a dedo release (default: the latest) over this command.
// It changes no repository: dedo upgrade does that, with the new release's
// templates.
func update(release string, out io.Writer) error {
	if release == "" {
		latest, err := latestRelease()
		if err != nil {
			return err
		}
		release = latest
	}
	if release == version {
		fmt.Fprintf(out, "dedo %s is installed; run dedo upgrade in a repository to move it to %s\n", version, version)
		return nil
	}
	fmt.Fprintf(out, "$ go install %s@%s\n", cliPkg, release)
	if err := goInstall(release, out); err != nil {
		return fmt.Errorf("installing dedo %s: %w", release, err)
	}
	fmt.Fprintf(out, "installed dedo %s at %s (was %s); run dedo upgrade in each repository\n", release, installedCLI(), version)
	return nil
}

// installedCLI is where go install puts dedo: $GOBIN, else $GOPATH/bin.
func installedCLI() string {
	name := "dedo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if bin, _ := exec.Command("go", "env", "GOBIN").Output(); strings.TrimSpace(string(bin)) != "" {
		return filepath.Join(strings.TrimSpace(string(bin)), name)
	}
	gopath, _ := exec.Command("go", "env", "GOPATH").Output()
	return filepath.Join(strings.TrimSpace(string(gopath)), "bin", name)
}

// upgrade moves a repository to this command's release, as one version:
// everything dedo init pins (the lint plugin, generators and runtime in
// go.mod, the options module's label in buf.yaml, Makefile.api), then
// buf.lock and the generated code.
func upgrade(dir string, offline bool, out io.Writer) error {
	_, goErr := os.Stat(filepath.Join(dir, "go.mod"))
	_, bufErr := os.Stat(filepath.Join(dir, "buf.yaml"))
	if errors.Is(goErr, os.ErrNotExist) && errors.Is(bufErr, os.ErrNotExist) {
		return fmt.Errorf("no go.mod or buf.yaml in %s: run dedo init first", dir)
	}
	if !offline {
		newerHint(out)
	}
	// A repository on connect-go v1 moves to v2 with connect-go's own
	// migration tool: one pass before regenerating, one after, as its guide
	// prescribes. What it cannot rewrite, it lists for a person to do.
	v1 := goErr == nil && usesConnectV1(dir)
	if v1 {
		fmt.Fprintln(out, "\nmigrating handlers and clients from connect-go v1 to v2 (connect-go-v2-migrate, pass 1)...")
		pre := &initer{p: &project{dir: dir}, out: out, cmds: [][]string{migrateCmd}}
		pre.runCommands(offline)
	}
	if err := initRepo(dir, version, offline, out); err != nil {
		return err
	}
	in := &initer{p: &project{dir: dir}, out: out}
	in.cmds = append(in.cmds, []string{"buf", "dep", "update"})
	if _, err := os.Stat(filepath.Join(dir, "buf.gen.yaml")); err == nil {
		in.cmds = append(in.cmds, []string{"buf", "generate"})
	}
	if v1 {
		in.cmds = append(in.cmds, migrateCmd)
	}
	if goErr == nil {
		in.cmds = append(in.cmds, []string{"go", "mod", "tidy"})
	}
	if failed := in.runCommands(offline); len(failed) > 0 {
		return fmt.Errorf("%d commands failed: %s; fix the cause and run dedo upgrade again", len(failed), strings.Join(failed, "; "))
	}
	fmt.Fprintf(out, "\nupgraded to dedo %s\n", version)
	return nil
}

// migrateCmd runs connect-go's v1-to-v2 migration tool, writing its changes.
var migrateCmd = []string{"go", "run", "connectrpc.com/connect/v2/cmd/connect-go-v2-migrate@v1.0.0", "-w"}

// usesConnectV1 reports whether go.mod requires connect-go v1.
func usesConnectV1(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && regexp.MustCompile(`(?m)^\s*(require\s+)?connectrpc\.com/connect v1\.`).Match(b)
}

// newerHint says when a later dedo is out, since upgrade moves a
// repository only to this command's own release.
func newerHint(out io.Writer) {
	if latest, err := latestRelease(); err == nil && newer(latest, version) {
		fmt.Fprintf(out, "dedo %s is out; this is %s. Run dedo update first to upgrade to it.\n\n", latest, version)
	}
}

// newer reports whether release a is later than b; both are vX.Y.Z.
func newer(a, b string) bool {
	pa, pb := semverParts(a), semverParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func semverParts(v string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	for i, p := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
