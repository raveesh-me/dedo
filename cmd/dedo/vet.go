package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/raveesh-me/dedo/lint/gorules"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// isVetTool reports whether go vet runs this command as its -vettool:
// it asks for the tool's version or flags, or hands it a package's
// configuration file.
func isVetTool(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "-V=full" || args[0] == "-flags" {
		return true
	}
	// go vet passes its flags, then the configuration file; a command of
	// dedo's never starts with a flag.
	return strings.HasSuffix(args[len(args)-1], ".cfg") && (len(args) == 1 || strings.HasPrefix(args[0], "-"))
}

// vetTool runs dedo's Go rules as go vet's -vettool, on one package.
func vetTool() {
	gorules.DocsRef = version
	unitchecker.Main(gorules.Analyzers()...)
}

// vet runs the Go rules on the Go module in dir, and on each backend for
// frontend nested in it, through go vet with this command as its -vettool,
// so go vet's cache skips packages that have not changed: dedo vet, which
// make check runs. dedo.yaml's go sections, with the files it imports,
// choose the rules.
func vet(dir string, out io.Writer) error {
	plan, root, err := vetPlan(dir)
	if err != nil {
		return err
	}
	tool, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding the dedo command for go vet: %w", err)
	}
	findings, err := vetModule(dir, tool, plan, root, false)
	if err != nil {
		return err
	}
	bffs, err := bffDirs(dir)
	if err != nil {
		return err
	}
	for _, b := range bffs {
		more, err := vetModule(filepath.Join(dir, filepath.FromSlash(b)), tool, plan, root, true)
		if err != nil {
			return fmt.Errorf("%s: %w", b, err)
		}
		findings = append(findings, more...)
	}
	for _, f := range findings {
		fmt.Fprintln(out, relPosn(dir, f))
	}
	if len(findings) > 0 {
		return fmt.Errorf("vet: %d finding(s); fix each, or suppress one with //dedo:ignore <RULE_ID> <reason>", len(findings))
	}
	fmt.Fprintf(out, "dedo vet: no findings (%d rules)\n", len(plan.Builtin)+len(plan.House))
	return nil
}

// vetPlan consolidates the Go rules of the repository's dedo.yaml, or
// dedo's own without one, and returns the folder house rules' allow_in
// globs are relative to.
func vetPlan(dir string) (gorules.Plan, string, error) {
	var sel config.GoSelection
	root, err := filepath.Abs(dir)
	if err != nil {
		return gorules.Plan{}, "", err
	}
	if path := config.Find(root); path != "" {
		c, err := config.Load(path)
		if err != nil {
			return gorules.Plan{}, "", fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		sel, root = c.Go, filepath.Dir(path)
	}
	plan, err := gorules.Consolidate(sel)
	if err != nil {
		return gorules.Plan{}, "", fmt.Errorf("go rules: %w", err)
	}
	return plan, root, nil
}

// vetModule runs go vet with the plan on every package of the module in
// dir.
func vetModule(dir, tool string, plan gorules.Plan, root string, bff bool) ([]gorules.Finding, error) {
	args, err := gorules.VetArgs(tool, plan, root, bff, []string{"./..."})
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if runErr != nil && strings.Contains(stderr.String(), "matched no packages") {
		return nil, nil // a module with no Go code yet
	}
	// go vet -json writes the diagnostics to stdout, and a # line per
	// package and build errors to stderr.
	findings, errs, err := gorules.ParseVet(&stdout)
	if runErr != nil || err != nil {
		return nil, fmt.Errorf("go vet: %w\n%s", errors.Join(runErr, err), strings.TrimSpace(stderr.String()))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("go vet: %s", strings.Join(errs, "\n"))
	}
	return findings, nil
}

// relPosn prints a finding with its file relative to dir.
func relPosn(dir string, f gorules.Finding) string {
	abs, err := filepath.Abs(dir)
	if err == nil {
		if rel, err := filepath.Rel(abs, f.Posn); err == nil && filepath.IsLocal(rel) {
			f.Posn = rel
		}
	}
	return f.String()
}
