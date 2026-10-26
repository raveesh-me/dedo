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

	"golang.org/x/mod/modfile"

	"github.com/raveesh-me/dedo/lint/house/config"
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

// pluginRulesFlag has the plugin report the consolidated rules and exit.
const pluginRulesFlag = "-dedo-rules"

func buildPluginCmd() []string {
	return []string{"go", "build", "-o", pluginPath(), lintPluginPkg}
}

// tools builds the lint plugin pinned in go.mod into .tools/, for a fresh
// clone, a CI job, or after the pin changes, then fetches and consolidates
// the house rules. It is what make tools does, without make.
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
	if err := buildPlugin(dir, out); err != nil {
		return err
	}
	return houseRules(dir, out)
}

// rulesVar is the lint plugin's variable that holds built-in house rules.
const rulesVar = "github.com/raveesh-me/dedo/lint/rules.embedded"

// wasmPlugin builds the lint plugin pinned in go.mod as WebAssembly into
// out, with the repository's house rules built in, since the WebAssembly
// plugin cannot read rules.yml. An organisation pushes it to the BSR for
// repositories without Go. tools has already checked the rules.
func wasmPlugin(dir, out string, w io.Writer) error {
	if err := checkEmbedSupport(dir); err != nil {
		return err
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	ldflags := "-s -w"
	if path := config.Find(dir); path != "" {
		c, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("rules.yml: %w", err)
		}
		embedded, err := config.Encode(c)
		if err != nil {
			return err
		}
		ldflags += " -X " + rulesVar + "=" + embedded
		fmt.Fprintf(w, "building rules.yml, with %d house rules, into the WebAssembly plugin\n", len(c.Rules))
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, lintPluginPkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	cmd.Stdout, cmd.Stderr = w, w
	fmt.Fprintf(w, "$ GOOS=wasip1 GOARCH=wasm go build -o %s %s\n", out, lintPluginPkg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the WebAssembly lint plugin: %w", err)
	}
	fmt.Fprintf(w, "built %s; push it with: buf plugin push <your BSR plugin> --binary=%s\n", out, out)
	return nil
}

// embedRelease is the first lint plugin release that reads built-in house
// rules; go build -X sets nothing in an earlier one, silently.
const embedRelease = "v0.15.0"

// checkEmbedSupport fails unless go.mod pins a lint plugin that reads
// built-in house rules.
func checkEmbedSupport(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		return err
	}
	for _, r := range f.Require {
		if r.Mod.Path == lintModule {
			if newer(embedRelease, r.Mod.Version) {
				return fmt.Errorf("-wasm needs the lint plugin at %s or later, and go.mod pins %s: run dedo upgrade", embedRelease, r.Mod.Version)
			}
			return nil
		}
	}
	return fmt.Errorf("go.mod does not pin %s: run dedo init", lintModule)
}

// lintModule is the module of the lint plugin.
const lintModule = "github.com/raveesh-me/dedo/lint"

// toolsLint moves only the lint plugin to release, then builds it and
// consolidates the house rules: dedo upgrade -lint.
func toolsLint(dir, release string, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no go.mod in %s: run dedo init first", dir)
	}
	c := []string{"go", "get", "-tool", lintPluginPkg + "@" + release}
	fmt.Fprintf(out, "$ %s\n", strings.Join(c, " "))
	cmd := exec.Command(c[0], c[1:]...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pinning the lint plugin: %w", err)
	}
	fmt.Fprintln(out, "go.mod requires the plugin's dedo runtime too, so Go moves github.com/raveesh-me/dedo with it if it was older; run dedo upgrade to move everything else")
	return tools(dir, out)
}

func buildPlugin(dir string, out io.Writer) error {
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

// houseRules fetches the git imports of the repository's rules.yml into
// .tools/rules/, where buf-plugin-dedo-lint reads them (the plugin never
// touches the network), then has the plugin consolidate every rule and
// report its overrides, so a mistake shows here rather than in the editor.
func houseRules(dir string, out io.Writer) error {
	if path := config.Find(dir); path != "" {
		fetched, err := config.Fetch(path)
		if err != nil {
			return fmt.Errorf("house rules: %w", err)
		}
		for _, f := range fetched {
			fmt.Fprintf(out, "fetched house rules %s\n", f)
		}
	}
	return consolidate(dir, out)
}

// consolidate runs the built plugin's -dedo-rules report in dir: rules.yml
// checked, and the rules in force.
func consolidate(dir string, out io.Writer) error {
	plugin, err := filepath.Abs(filepath.Join(dir, pluginPath()))
	if err != nil {
		return err
	}
	if _, err := os.Stat(plugin); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no lint plugin at %s: run dedo tools", pluginPath())
	}
	cmd := exec.Command(plugin, pluginRulesFlag)
	cmd.Dir = dir
	report, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		fmt.Fprintf(out, "lint rules: %s", report)
		return nil
	case strings.Contains(string(report), "unknown shorthand flag"):
		// A plugin from before overrides cannot report them.
		fmt.Fprintf(out, "%s predates rule overrides, so it cannot list them: run dedo upgrade\n", pluginPath())
		return nil
	case !errors.As(err, &exit):
		return fmt.Errorf("running %s: %w", pluginPath(), err)
	}
	return fmt.Errorf("the lint rules do not consolidate:\n%s", strings.TrimSpace(string(report)))
}
