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

	"gopkg.in/yaml.v3"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// lintPluginPkg is the lint plugin's command.
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

// tools vendors the lint plugin at the options module's release in
// buf.yaml into .tools/, for a fresh clone, a CI job, or after the pin
// changes, then fetches and consolidates the house rules. The plugin
// belongs to the protos, so it is the same whether or not a Go module sits
// beside them. It is what make tools does, without make.
func tools(dir string, out io.Writer) error {
	release, err := pinnedRelease(dir)
	if err != nil {
		return err
	}
	if err := vendorPlugin(dir, release, out); err != nil {
		return err
	}
	return houseRules(dir, out)
}

// rulesVar is the lint plugin's variable that holds built-in house rules.
const rulesVar = "github.com/raveesh-me/dedo/lint/rules.embedded"

// wasmPlugin builds the lint plugin at buf.yaml's release as WebAssembly
// into out, with the repository's house rules built in, since the
// WebAssembly plugin cannot read dedo.yaml. An organisation pushes it to the
// BSR for machines that cannot run the native plugin. tools has already
// checked the rules.
func wasmPlugin(dir, out string, w io.Writer) error {
	release, err := pinnedRelease(dir)
	if err != nil {
		return err
	}
	if err := checkEmbedSupport(release); err != nil {
		return err
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	ldflags := "-s -w"
	if path := config.Find(dir); path != "" {
		c, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		embedded, err := config.Encode(c)
		if err != nil {
			return err
		}
		ldflags += " -X " + rulesVar + "=" + embedded
		fmt.Fprintf(w, "building %s, with %d house rules, into the WebAssembly plugin\n", filepath.Base(path), len(c.Rules))
	}
	// A throwaway module pins the release, so no go.mod of the
	// repository's is needed or changed.
	mod, err := os.MkdirTemp("", "dedo-wasm")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mod)
	for _, c := range [][]string{
		{"go", "mod", "init", "dedo-wasm"},
		{"go", "get", lintPluginPkg + "@" + release},
		{"go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, lintPluginPkg},
	} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = mod
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		cmd.Stdout, cmd.Stderr = w, w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("building the WebAssembly lint plugin (%s): %w", strings.Join(c[:2], " "), err)
		}
	}
	fmt.Fprintf(w, "built %s at %s; push it with: buf plugin push <your BSR plugin> --binary=%s\n", out, release, out)
	return nil
}

// embedRelease is the first lint plugin release that reads built-in house
// rules; go build -X sets nothing in an earlier one, silently.
const embedRelease = "v0.15.0"

// checkEmbedSupport fails unless the lint plugin at release reads built-in
// house rules.
func checkEmbedSupport(release string) error {
	if newer(embedRelease, release) {
		return fmt.Errorf("-wasm needs the lint plugin at %s or later, and buf.yaml pins %s: run dedo upgrade", embedRelease, release)
	}
	return nil
}

// toolsLint moves only the options module, whose label names the lint
// plugin's release, to release, then vendors the plugin and consolidates
// the house rules: dedo upgrade -lint.
func toolsLint(dir, release string, offline bool, out io.Writer) error {
	doc, ok, err := loadYAML(filepath.Join(dir, "buf.yaml"))
	if err != nil {
		return fmt.Errorf("buf.yaml: %w", err)
	}
	if !ok {
		return fmt.Errorf("no buf.yaml in %s: run dedo init first", dir)
	}
	in := &initer{p: &project{dir: dir}, release: release, out: out}
	if pinned := in.pinOptions(child(root(doc), "deps", yaml.SequenceNode)); pinned != "" {
		if err := saveYAML(filepath.Join(dir, "buf.yaml"), doc); err != nil {
			return err
		}
		in.say("buf.yaml: options module pinned to %s, the lint plugin's release", pinned)
	}
	in.depUpdate()
	if failed := in.runCommands(offline); len(failed) > 0 {
		return fmt.Errorf("%s failed; fix the cause and run dedo upgrade -lint again", strings.Join(failed, "; "))
	}
	if offline {
		in.say("run dedo tools in %s: it vendors the lint plugin at %s", dir, release)
		return nil
	}
	return tools(dir, out)
}

// houseRules fetches the git imports of the repository's dedo.yaml into
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

// consolidate runs the built plugin's -dedo-rules report in dir: dedo.yaml
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
