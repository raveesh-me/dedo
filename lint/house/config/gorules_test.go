package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const platformRule = `go:
  rules:
    - id: HOUSE_GO_NO_PLATFORM_SDK
      forbid_import: example.com/platform-sdk/...
      allow_in: [internal/legacy/**]
      message: Call the platform services directly.
      doc: RULES.html#house_go_no_platform_sdk
`

func goIDs(rules []GoRule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}

// A dedo.yaml's go section holds Go rules of its own and those of the files
// it imports, ranked like lint rules, with use and disable of its own.
func TestLoadGoRules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"standards/rules.yml": platformRule + `    - id: HOUSE_GO_NO_UNSAFE
      forbid_import: unsafe
      message: No unsafe.
`,
		FileName: `imports:
  - path: standards
go:
  use: [DEDO, HOUSE]
  disable: [HOUSE_GO_NO_UNSAFE, DEDO_GO_EVENT_DECLARED]
  rules:
    - id: HOUSE_GO_NO_LEGACY
      forbid_import: example.com/legacy
      message: Not the legacy client.
      overrides: [HOUSE_GO_NO_PLATFORM_SDK]
      final: true
`,
	})
	c, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := goIDs(c.Go.Rules); !slices.Equal(got, []string{"HOUSE_GO_NO_PLATFORM_SDK", "HOUSE_GO_NO_LEGACY"}) {
		t.Errorf("rules = %v", got)
	}
	if !slices.Equal(c.Go.Use, []string{"DEDO", "HOUSE"}) {
		t.Errorf("use = %v", c.Go.Use)
	}
	if !slices.Equal(c.Go.Disabled, []string{"HOUSE_GO_NO_UNSAFE", "DEDO_GO_EVENT_DECLARED"}) {
		t.Errorf("disabled = %v", c.Go.Disabled)
	}
	sdk, own := c.Go.Rules[0], c.Go.Rules[1]
	if !slices.Equal(sdk.AllowIn, []string{"internal/legacy/**"}) || sdk.ForbidImport != "example.com/platform-sdk/..." {
		t.Errorf("imported rule = %+v", sdk)
	}
	top, _ := filepath.Abs(filepath.Join(dir, FileName))
	if !slices.Equal(sdk.Importers, []string{top}) || own.Importers != nil {
		t.Errorf("importers: %v and %v", sdk.Importers, own.Importers)
	}
	if !own.Final || !slices.Equal(own.Overrides, []string{"HOUSE_GO_NO_PLATFORM_SDK"}) || own.Source != filepath.Join(dir, FileName) {
		t.Errorf("own rule = %+v", own)
	}
	if len(c.Lint.Rules) != 0 || len(c.Breaking.Rules) != 0 {
		t.Errorf("go rules leaked into lint or breaking: %v %v", c.Lint.Rules, c.Breaking.Rules)
	}
}

func TestGoRuleMatches(t *testing.T) {
	r := GoRule{ForbidImport: "example.com/sdk/..."}
	for path, want := range map[string]bool{
		"example.com/sdk":          true,
		"example.com/sdk/auth":     true,
		"example.com/sdk/auth/v2":  true,
		"example.com/sdkx":         false,
		"example.com/other":        false,
		"example.com/sdk-internal": false,
	} {
		if got := r.Forbids(path); got != want {
			t.Errorf("%s: forbids = %v, want %v", path, got, want)
		}
	}
	exact := GoRule{ForbidImport: "unsafe"}
	if !exact.Forbids("unsafe") || exact.Forbids("unsafe/x") {
		t.Error("an exact path forbids only itself")
	}
}

func TestGoRuleAllows(t *testing.T) {
	r := GoRule{AllowIn: []string{"internal/legacy/**", "cmd/*/main.go", "**/*_test.go"}}
	for file, want := range map[string]bool{
		"internal/legacy/client.go":     true,
		"internal/legacy/deep/x/y.go":   true,
		"internal/legacy.go":            false,
		"internal/legacyx/a.go":         false,
		"cmd/tool/main.go":              true,
		"cmd/tool/other.go":             false,
		"cmd/a/b/main.go":               false,
		"internal/orders/store_test.go": true,
		"store_test.go":                 true,
		"internal/orders/store.go":      false,
	} {
		if got := r.Allows(file); got != want {
			t.Errorf("%s: allows = %v, want %v", file, got, want)
		}
	}
}

func TestLoadGoRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"bad id":         {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_X, forbid_import: unsafe, message: x}\n"}, "must look like HOUSE_GO_"},
		"no kind":        {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, message: x}\n"}, "forbid_import"},
		"bad path":       {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: 'a b', message: x}\n"}, "not an import path"},
		"bad wildcard":   {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: 'a/.../b', message: x}\n"}, "not an import path"},
		"no message":     {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe}\n"}, "no message"},
		"bad glob":       {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x, allow_in: ['[']}\n"}, "allow_in"},
		"absolute glob":  {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x, allow_in: [/etc/*]}\n"}, "allow_in"},
		"unknown key":    {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x, severity: high}\n"}, "severity"},
		"duplicate":      {map[string]string{RulesFileName: "imports:\n  - path: b.yml\n" + platformRule, "b.yml": platformRule}, "defined twice"},
		"lint clash":     {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x}\nlint:\n  rules:\n  - {id: HOUSE_GO_X, on: field, check: \"true\", message: x}\n"}, "defined twice"},
		"disable":        {map[string]string{RulesFileName: "go:\n  disable: [HOUSE_GO_GONE]\n"}, "no rules file defines"},
		"disable lint":   {map[string]string{RulesFileName: "go:\n  disable: [HOUSE_X]\nlint:\n  rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x}\n"}, "not a go rule"},
		"disable id":     {map[string]string{RulesFileName: "go:\n  disable: [nope]\n"}, "not a rule ID"},
		"imported use":   {map[string]string{RulesFileName: "imports:\n  - path: b.yml\n", "b.yml": "go:\n  use: [DEDO]\n"}, "go.use belongs in the repository's own dedo.yaml"},
		"override id":    {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x, overrides: [nope]}\n"}, "not a rule ID"},
		"override self":  {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: unsafe, message: x, overrides: [HOUSE_GO_X]}\n"}, "overrides itself"},
		"disable final":  {map[string]string{RulesFileName: "imports:\n  - path: b.yml\ngo:\n  disable: [HOUSE_GO_LOCKED]\n", "b.yml": "go:\n  rules:\n  - {id: HOUSE_GO_LOCKED, forbid_import: unsafe, message: x, final: true}\n"}, "b.yml marks final"},
		"go is no field": {map[string]string{RulesFileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, on: field, forbid_import: unsafe, message: x}\n"}, "doesn't know `on` in a go rule"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.files)
			if _, err := Load(filepath.Join(dir, RulesFileName)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A url-imported rules file's relative doc resolves against its URL, for
// Go rules as for lint rules.
func TestGoRuleDocFromURLImport(t *testing.T) {
	base := serve(t, map[string]string{"/rules/v1/rules.yml": platformRule})
	path := importURL(t, base+"/rules/v1/rules.yml", sum(platformRule))
	if _, err := Fetch(path); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Go.Rules[0].Doc, base+"/rules/v1/RULES.html#house_go_no_platform_sdk"; got != want {
		t.Errorf("doc = %q, want %q", got, want)
	}
}

// Go rules survive Encode and Decode, which the WebAssembly plugin's
// built-in rules go through, so a dedo.yaml with a go section still builds.
func TestEncodeGoRules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{FileName: platformRule})
	c, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := goIDs(back.Go.Rules); !slices.Equal(got, []string{"HOUSE_GO_NO_PLATFORM_SDK"}) || back.Go.Rules[0].ForbidImport != "example.com/platform-sdk/..." {
		t.Errorf("decoded = %+v", back.Go)
	}
}
