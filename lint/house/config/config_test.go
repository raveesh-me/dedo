package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const domainRule = `rules:
  - id: HOUSE_RESOURCE_TYPE_DOMAIN
    on: resource
    check: type.endsWith('.acme.com')
    message: Resource types live under acme.com.
`

func ids(rules []Rule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"buf.yaml": "version: v2\n", "proto/x.proto": ""})
	if got := Find(filepath.Join(dir, "proto")); got != "" {
		t.Errorf("without rules.yml: %q", got)
	}
	write(t, dir, map[string]string{RulesFileName: domainRule})
	if got := Find(filepath.Join(dir, "proto")); got != filepath.Join(dir, RulesFileName) {
		t.Errorf("Find = %q", got)
	}
	t.Setenv(EnvFile, "/elsewhere/rules.yml")
	if got := Find(dir); got != "/elsewhere/rules.yml" {
		t.Errorf("with %s: %q", EnvFile, got)
	}
}

func TestLoadImportsAndDisable(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"standards/rules.yml": domainRule + `  - id: HOUSE_PLURAL_COLLECTIONS
    on: resource
    check: plural != ''
    message: Name the plural.
`,
		"svc/extra.yml": "rules:\n  - id: HOUSE_LOCAL\n    on: method\n    check: \"true\"\n    message: Local.\n",
		"svc/rules.yml": `imports:
  - path: ../standards
  - path: extra.yml
disable: [HOUSE_PLURAL_COLLECTIONS]
rules:
  - id: HOUSE_OWN
    on: field
    check: "!name.endsWith('_id')"
    message: No id fields.
`,
	})
	rules, err := loadRules(filepath.Join(dir, "svc", RulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rules); !slices.Equal(got, []string{"HOUSE_RESOURCE_TYPE_DOMAIN", "HOUSE_LOCAL", "HOUSE_OWN"}) {
		t.Errorf("rules = %v", got)
	}
	if !strings.HasSuffix(rules[0].Source, filepath.Join("standards", "rules.yml")) {
		t.Errorf("source = %s", rules[0].Source)
	}
}

// A rule's importers are every file that imports its file, directly or
// through others: the files whose rules outrank it.
func TestLoadRanksImporters(t *testing.T) {
	dir := t.TempDir()
	rule := func(id string) string {
		return "  - {id: " + id + ", on: field, check: \"true\", message: x}\n"
	}
	write(t, dir, map[string]string{
		"base.yml":  "rules:\n" + rule("HOUSE_BASE"),
		"mid.yml":   "imports:\n  - path: base.yml\nrules:\n" + rule("HOUSE_MID"),
		"other.yml": "imports:\n  - path: base.yml\nrules:\n" + rule("HOUSE_OTHER"),
		RulesFileName: "imports:\n  - path: mid.yml\n  - path: other.yml\nrules:\n" +
			"  - {id: HOUSE_TOP, on: field, check: \"true\", message: x, overrides: [HOUSE_MID, DEDO_HTTP_PATH_VERSION]}\n",
	})
	rules, err := loadRules(filepath.Join(dir, RulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	abs := func(names ...string) []string {
		var out []string
		for _, n := range names {
			p, _ := filepath.Abs(filepath.Join(dir, n))
			out = append(out, p)
		}
		slices.Sort(out)
		return out
	}
	want := map[string][]string{
		"HOUSE_BASE":  abs("mid.yml", "other.yml", RulesFileName),
		"HOUSE_MID":   abs(RulesFileName),
		"HOUSE_OTHER": abs(RulesFileName),
		"HOUSE_TOP":   nil,
	}
	for _, r := range rules {
		if !slices.Equal(r.Importers, want[r.ID]) {
			t.Errorf("%s: importers %v, want %v", r.ID, r.Importers, want[r.ID])
		}
		if r.ID == "HOUSE_TOP" && !slices.Equal(r.Overrides, []string{"HOUSE_MID", "DEDO_HTTP_PATH_VERSION"}) {
			t.Errorf("overrides = %v", r.Overrides)
		}
	}
}

func TestLoadRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"cycle":         {map[string]string{RulesFileName: "imports:\n  - path: b.yml\n", "b.yml": "imports:\n  - path: rules.yml\n"}, "import each other"},
		"duplicate":     {map[string]string{RulesFileName: "imports:\n  - path: b.yml\n" + domainRule, "b.yml": domainRule}, "defined twice"},
		"bad id":        {map[string]string{RulesFileName: "rules:\n  - {id: NOPE, on: field, check: \"true\", message: x}\n"}, "must look like"},
		"bad kind":      {map[string]string{RulesFileName: "rules:\n  - {id: HOUSE_X, on: rpc, check: \"true\", message: x}\n"}, "want one of"},
		"no check":      {map[string]string{RulesFileName: "rules:\n  - {id: HOUSE_X, on: field, message: x}\n"}, "no check"},
		"unknown key":   {map[string]string{RulesFileName: "rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x, severity: high}\n"}, "severity"},
		"disable":       {map[string]string{RulesFileName: "disable: [HOUSE_GONE]\n"}, "HOUSE_GONE"},
		"disable id":    {map[string]string{RulesFileName: "disable: [aip-131]\n"}, "not a rule ID"},
		"imported use":  {map[string]string{RulesFileName: "imports:\n  - path: b.yml\n", "b.yml": "use: [DEDO]\n"}, "use belongs in the repository's own dedo.yaml"},
		"override id":   {map[string]string{RulesFileName: "rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x, overrides: [aip-131]}\n"}, "not a rule ID"},
		"disable final": {map[string]string{RulesFileName: "imports:\n  - path: b.yml\ndisable: [HOUSE_LOCKED]\n", "b.yml": "rules:\n  - {id: HOUSE_LOCKED, on: field, check: \"true\", message: x, final: true}\n"}, "b.yml marks final"},
		"override self": {map[string]string{RulesFileName: "rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x, overrides: [HOUSE_X]}\n"}, "overrides itself"},
		"git ref":       {map[string]string{RulesFileName: "imports:\n  - git: https://example.com/r.git\n"}, "needs a ref"},
		"git unfetched": {map[string]string{RulesFileName: "imports:\n  - git: https://example.com/r.git\n    ref: v1\n"}, "run dedo tools"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.files)
			if _, err := loadRules(filepath.Join(dir, RulesFileName)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestEmptyRulesFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: ""})
	if rules, err := loadRules(filepath.Join(dir, RulesFileName)); err != nil || len(rules) != 0 {
		t.Errorf("rules %v, err %v", rules, err)
	}
}

// A team publishes rules in a git repository; services import them at a
// tag, and dedo tools fetches that tag.
func TestFetchGitImport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	remote := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", remote, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet")
	write(t, remote, map[string]string{"api/rules.yml": domainRule})
	git("add", ".")
	git("commit", "--quiet", "-m", "v1")
	git("tag", "v1")

	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: "imports:\n  - git: " + remote + "\n    ref: v1\n    path: api/rules.yml\n"})
	if _, err := loadRules(filepath.Join(dir, RulesFileName)); err == nil || !strings.Contains(err.Error(), "run dedo tools") {
		t.Fatalf("before fetching: %v", err)
	}
	fetched, err := Fetch(filepath.Join(dir, RulesFileName))
	if err != nil || len(fetched) != 1 {
		t.Fatalf("Fetch = %v, %v", fetched, err)
	}
	rules, err := loadRules(filepath.Join(dir, RulesFileName))
	if err != nil || !slices.Equal(ids(rules), []string{"HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Fatalf("after fetching: %v, %v", ids(rules), err)
	}
	if again, err := Fetch(filepath.Join(dir, RulesFileName)); err != nil || len(again) != 0 {
		t.Errorf("second Fetch = %v, %v; want nothing new", again, err)
	}
}

// remoteRepo commits files, symlinks included, to a new git repository
// tagged v1, and returns its directory.
func remoteRepo(t *testing.T, files map[string]string, links map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	remote := t.TempDir()
	write(t, remote, files)
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(remote, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."}, {"commit", "--quiet", "-m", "v1"}, {"tag", "v1"}} {
		cmd := exec.Command("git", append([]string{"-C", remote, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return remote
}

// fetchAndLoad imports remote's file at v1 from a service's rules.yml.
func fetchAndLoad(t *testing.T, remote, path string) ([]Rule, error) {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: "imports:\n  - git: " + remote + "\n    ref: v1\n    path: " + path + "\n"})
	if _, err := Fetch(filepath.Join(dir, RulesFileName)); err != nil {
		return nil, err
	}
	return loadRules(filepath.Join(dir, RulesFileName))
}

// In the service's own files too, a path import names a file from the root,
// the folder of rules.yml, wherever the importing file sits.
func TestLocalImportPathsFromRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		RulesFileName:               "imports:\n  - path: platform-rules/rules.yml\n",
		"platform-rules/rules.yml":  "imports:\n  - path: platform-rules/naming.yml\n" + domainRule,
		"platform-rules/naming.yml": "rules:\n  - {id: HOUSE_NAMING, on: field, check: \"true\", message: Naming.}\n",
	})
	rules, err := loadRules(filepath.Join(dir, RulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rules); !slices.Equal(got, []string{"HOUSE_NAMING", "HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Errorf("rules = %v", got)
	}
}

// Inside a shared repository, a path import names a file from that
// repository's root, wherever the importing file sits.
func TestGitImportPathsFromRepoRoot(t *testing.T) {
	remote := remoteRepo(t, map[string]string{
		"api/rules.yml":     "imports:\n  - path: shared/naming.yml\n  - path: shared\n" + domainRule,
		"shared/naming.yml": "rules:\n  - {id: HOUSE_NAMING, on: field, check: \"true\", message: Naming.}\n",
		"shared/rules.yml":  "rules:\n  - {id: HOUSE_SHARED, on: field, check: \"true\", message: Shared.}\n",
		"api/shared/x.yml":  "rules:\n  - {id: HOUSE_WRONG, on: field, check: \"true\", message: Wrong.}\n",
	}, nil)
	rules, err := fetchAndLoad(t, remote, "api/rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rules); !slices.Equal(got, []string{"HOUSE_NAMING", "HOUSE_SHARED", "HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Errorf("rules = %v", got)
	}
}

// A shared repository's rules cannot reach outside it, into the service or
// anywhere else on the machine that runs the lint.
func TestGitImportCannotLeaveRepo(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.yml")
	write(t, filepath.Dir(outside), map[string]string{"secret.yml": domainRule})
	for name, tc := range map[string]struct {
		files map[string]string
		links map[string]string
		path  string
	}{
		"dot-dot":       {files: map[string]string{"rules.yml": "imports:\n  - path: ../../../secret.yml\n"}, path: "rules.yml"},
		"absolute":      {files: map[string]string{"rules.yml": "imports:\n  - path: " + outside + "\n"}, path: "rules.yml"},
		"symlink":       {files: map[string]string{"rules.yml": "imports:\n  - path: evil.yml\n"}, links: map[string]string{"evil.yml": outside}, path: "rules.yml"},
		"git path":      {files: map[string]string{"rules.yml": domainRule}, path: "../../../secret.yml"},
		"git path link": {links: map[string]string{"rules.yml": outside}, path: "rules.yml"},
	} {
		t.Run(name, func(t *testing.T) {
			remote := remoteRepo(t, tc.files, tc.links)
			if _, err := fetchAndLoad(t, remote, tc.path); err == nil || !strings.Contains(err.Error(), "outside") {
				t.Fatalf("err = %v, want an import outside the repository refused", err)
			}
		})
	}
}

func loadRules(path string) ([]Rule, error) {
	c, err := Load(path)
	return c.Rules, err
}

// use and the disables of dedo's own rules pass through, for package rules
// to check against the rules it has.
func TestLoadUse(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: "use: [AIP_CORE, DEDO]\ndisable: [DEDO_NO_JSON_NAME]\n"})
	c, err := Load(filepath.Join(dir, RulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Use, []string{"AIP_CORE", "DEDO"}) || !slices.Equal(c.Disabled, []string{"DEDO_NO_JSON_NAME"}) {
		t.Errorf("use %v, disabled %v", c.Use, c.Disabled)
	}
}

// dedo.yaml keeps the rules in its rules section, and wins over a rules.yml
// beside it, which a repository from before dedo.yaml still has.
func TestDedoYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"buf.yaml":    "version: v2\n",
		RulesFileName: "rules: []\n",
		"shared/rules.yml": `rules:
  - id: HOUSE_SHARED
    on: file
    check: "true"
    message: m
`,
	})
	if got := Find(dir); got != filepath.Join(dir, RulesFileName) {
		t.Errorf("without dedo.yaml: Find = %q", got)
	}
	write(t, dir, map[string]string{FileName: "rules:\n  use: [DEDO, HOUSE]\n  imports:\n    - path: shared\n" +
		"  " + strings.ReplaceAll(domainRule, "\n", "\n  ") + "\nannexure:\n  - path: docs/agents.md\n"})
	path := Find(dir)
	if path != filepath.Join(dir, FileName) {
		t.Fatalf("Find = %q, want dedo.yaml", path)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Use, []string{"DEDO", "HOUSE"}) || !slices.Equal(ids(c.Rules), []string{"HOUSE_SHARED", "HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Errorf("use %v, rules %v", c.Use, ids(c.Rules))
	}
	fetched, err := Fetch(path)
	if err != nil || len(fetched) != 0 {
		t.Errorf("Fetch = %v, %v", fetched, err)
	}

	// A rules file in dedo.yaml's place is a mistake, not a rules file.
	write(t, dir, map[string]string{FileName: domainRule})
	if _, err := Load(path); err == nil {
		t.Error("dedo.yaml without a rules section: no error")
	}
	// Unknown sections are rejected, as unknown keys in a rules file are.
	write(t, dir, map[string]string{FileName: "rule:\n  use: [DEDO]\n"})
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "rule") {
		t.Errorf("unknown section: err = %v", err)
	}
}
