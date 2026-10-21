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
	write(t, dir, map[string]string{FileName: "imports:\n  - git: " + remote + "\n    ref: v1\n    path: " + path + "\n"})
	if _, err := Fetch(filepath.Join(dir, FileName)); err != nil {
		return nil, err
	}
	return Load(filepath.Join(dir, FileName))
}

// In the service's own files too, a path import names a file from the root,
// the folder of rules.yml, wherever the importing file sits.
func TestLocalImportPathsFromRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		FileName:                    "imports:\n  - path: platform-rules/rules.yml\n",
		"platform-rules/rules.yml":  "imports:\n  - path: platform-rules/naming.yml\n" + domainRule,
		"platform-rules/naming.yml": "rules:\n  - {id: HOUSE_NAMING, on: field, check: \"true\", message: Naming.}\n",
	})
	rules, err := Load(filepath.Join(dir, FileName))
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
