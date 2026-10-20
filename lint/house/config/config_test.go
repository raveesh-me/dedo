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
	write(t, dir, map[string]string{FileName: domainRule})
	if got := Find(filepath.Join(dir, "proto")); got != filepath.Join(dir, FileName) {
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
	rules, err := Load(filepath.Join(dir, "svc", FileName))
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

func TestLoadRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"cycle":         {map[string]string{FileName: "imports:\n  - path: b.yml\n", "b.yml": "imports:\n  - path: rules.yml\n"}, "import each other"},
		"duplicate":     {map[string]string{FileName: "imports:\n  - path: b.yml\n" + domainRule, "b.yml": domainRule}, "defined twice"},
		"bad id":        {map[string]string{FileName: "rules:\n  - {id: NOPE, on: field, check: \"true\", message: x}\n"}, "must look like"},
		"bad kind":      {map[string]string{FileName: "rules:\n  - {id: HOUSE_X, on: rpc, check: \"true\", message: x}\n"}, "want one of"},
		"no check":      {map[string]string{FileName: "rules:\n  - {id: HOUSE_X, on: field, message: x}\n"}, "no check"},
		"unknown key":   {map[string]string{FileName: "rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x, severity: high}\n"}, "severity"},
		"disable":       {map[string]string{FileName: "disable: [HOUSE_GONE]\n"}, "HOUSE_GONE"},
		"git ref":       {map[string]string{FileName: "imports:\n  - git: https://example.com/r.git\n"}, "needs a ref"},
		"git unfetched": {map[string]string{FileName: "imports:\n  - git: https://example.com/r.git\n    ref: v1\n"}, "run dedo tools"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.files)
			if _, err := Load(filepath.Join(dir, FileName)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestEmptyRulesFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{FileName: ""})
	if rules, err := Load(filepath.Join(dir, FileName)); err != nil || len(rules) != 0 {
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
	write(t, dir, map[string]string{FileName: "imports:\n  - git: " + remote + "\n    ref: v1\n    path: api/rules.yml\n"})
	if _, err := Load(filepath.Join(dir, FileName)); err == nil || !strings.Contains(err.Error(), "run dedo tools") {
		t.Fatalf("before fetching: %v", err)
	}
	fetched, err := Fetch(filepath.Join(dir, FileName))
	if err != nil || len(fetched) != 1 {
		t.Fatalf("Fetch = %v, %v", fetched, err)
	}
	rules, err := Load(filepath.Join(dir, FileName))
	if err != nil || !slices.Equal(ids(rules), []string{"HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Fatalf("after fetching: %v, %v", ids(rules), err)
	}
	if again, err := Fetch(filepath.Join(dir, FileName)); err != nil || len(again) != 0 {
		t.Errorf("second Fetch = %v, %v; want nothing new", again, err)
	}
}
