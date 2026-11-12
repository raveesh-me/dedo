package gorules

import (
	"slices"
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// rule is a house Go rule of file, imported by importers.
func rule(id, file string, importers ...string) config.GoRule {
	return config.GoRule{ID: id, ForbidImport: "example.com/x", Message: "m", Source: file, File: "/" + file, Importers: importers}
}

func houseIDs(rules []config.GoRule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}

func allBuiltin() []string {
	var out []string
	for _, b := range Builtins() {
		out = append(out, b.ID)
	}
	return out
}

func TestConsolidateDefault(t *testing.T) {
	p, err := Consolidate(config.GoSelection{Rules: []config.GoRule{rule("HOUSE_GO_A", "dedo.yaml")}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.DefaultUse || !slices.Equal(p.Builtin, allBuiltin()) || !slices.Equal(houseIDs(p.House), []string{"HOUSE_GO_A"}) {
		t.Errorf("plan = %+v", p)
	}
	report := p.Report()
	for _, want := range []string{
		"go: 5 rules, 1 of them house rules; 5 run, 0 overridden, 0 not used or disabled",
		"use: DEDO, HOUSE (the default; dedo.yaml does not say go.use)",
		"HOUSE_GO_A (dedo.yaml) forbids importing example.com/x",
		"DEDO_GO_IGNORE_REASON is final",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
}

func TestConsolidateUseAndDisable(t *testing.T) {
	p, err := Consolidate(config.GoSelection{
		Use:      []string{SetDedo},
		Disabled: []string{IDEventDeclared},
		Rules:    []config.GoRule{rule("HOUSE_GO_A", "dedo.yaml")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{IDServerInterceptors, IDErrorReasonDeclared, IDIgnoreReason}; !slices.Equal(p.Builtin, want) {
		t.Errorf("builtin = %v, want %v", p.Builtin, want)
	}
	if len(p.House) != 0 || p.Off["HOUSE_GO_A"] != "Not in dedo.yaml's go.use." || p.Off[IDEventDeclared] != "Disabled in dedo.yaml's go section." {
		t.Errorf("plan = %+v", p)
	}
	if !strings.Contains(p.Report(), "DEDO_GO_EVENT_DECLARED is disabled") {
		t.Errorf("report:\n%s", p.Report())
	}
	// A rule named by ID runs without its set.
	p, err = Consolidate(config.GoSelection{Use: []string{"HOUSE_GO_A"}, Rules: []config.GoRule{rule("HOUSE_GO_A", "dedo.yaml")}})
	if err != nil || !slices.Equal(p.Builtin, []string{IDIgnoreReason}) || len(p.House) != 1 {
		t.Errorf("use by ID: %+v, %v", p, err)
	}
}

// A rule of a file that imports another overrides that file's rules and
// dedo's; an override lapses when its rule is overridden itself.
func TestConsolidateOverrides(t *testing.T) {
	base := rule("HOUSE_GO_BASE", "base.yml", "/mid.yml", "/dedo.yaml")
	mid := rule("HOUSE_GO_MID", "mid.yml", "/dedo.yaml")
	mid.Overrides = []string{"HOUSE_GO_BASE", IDEventDeclared}
	top := rule("HOUSE_GO_TOP", "dedo.yaml")
	top.Overrides = []string{"HOUSE_GO_MID"}
	p, err := Consolidate(config.GoSelection{Rules: []config.GoRule{base, mid, top}})
	if err != nil {
		t.Fatal(err)
	}
	if got := houseIDs(p.House); !slices.Equal(got, []string{"HOUSE_GO_BASE", "HOUSE_GO_TOP"}) {
		t.Errorf("house rules that run = %v", got)
	}
	if !slices.Contains(p.Builtin, IDEventDeclared) {
		t.Errorf("a lapsed override still silences %s", IDEventDeclared)
	}
	if p.Off["HOUSE_GO_MID"] != "Overridden by HOUSE_GO_TOP." {
		t.Errorf("off = %v", p.Off)
	}
	report := p.Report()
	for _, want := range []string{
		"HOUSE_GO_MID (mid.yml) overrides DEDO_GO_EVENT_DECLARED, lapsed: HOUSE_GO_MID is overridden itself",
		"HOUSE_GO_TOP (dedo.yaml) overrides HOUSE_GO_MID\n",
		"1 overridden",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	// Without the top rule, the middle one overrides both.
	p, err = Consolidate(config.GoSelection{Rules: []config.GoRule{base, mid}})
	if err != nil || slices.Contains(p.Builtin, IDEventDeclared) || !slices.Equal(houseIDs(p.House), []string{"HOUSE_GO_MID"}) {
		t.Errorf("plan = %+v, %v", p, err)
	}
}

func TestConsolidateRejects(t *testing.T) {
	final := rule("HOUSE_GO_LOCKED", "base.yml", "/dedo.yaml")
	final.Final = true
	sideways := rule("HOUSE_GO_SIDE", "side.yml", "/dedo.yaml")
	sideways.Overrides = []string{"HOUSE_GO_LOCKED"}
	up := rule("HOUSE_GO_BASE", "base.yml", "/dedo.yaml")
	up.Overrides = []string{"HOUSE_GO_TOP"}
	withOverride := func(r config.GoRule, ids ...string) config.GoRule {
		r.Overrides = ids
		return r
	}
	for name, tc := range map[string]struct {
		sel  config.GoSelection
		want string
	}{
		"unknown set":     {config.GoSelection{Use: []string{"AIP"}}, "go.use names AIP"},
		"unknown disable": {config.GoSelection{Disabled: []string{"DEDO_GO_NOPE"}}, "go.disable names DEDO_GO_NOPE, which no go rule defines"},
		"disable final":   {config.GoSelection{Disabled: []string{IDIgnoreReason}}, "is final"},
		"unknown target":  {config.GoSelection{Rules: []config.GoRule{withOverride(rule("HOUSE_GO_A", "dedo.yaml"), "DEDO_HTTP_PATH_VERSION")}}, "a go rule overrides only go rules"},
		"final builtin":   {config.GoSelection{Rules: []config.GoRule{withOverride(rule("HOUSE_GO_A", "dedo.yaml"), IDIgnoreReason)}}, "which is final"},
		"final house":     {config.GoSelection{Rules: []config.GoRule{final, withOverride(rule("HOUSE_GO_A", "dedo.yaml"), "HOUSE_GO_LOCKED")}}, "marked final in base.yml"},
		"sideways":        {config.GoSelection{Rules: []config.GoRule{rule("HOUSE_GO_LOCKED", "base.yml", "/dedo.yaml"), sideways}}, "side.yml: HOUSE_GO_SIDE cannot override HOUSE_GO_LOCKED"},
		"upwards":         {config.GoSelection{Rules: []config.GoRule{up, rule("HOUSE_GO_TOP", "dedo.yaml")}}, "cannot override HOUSE_GO_TOP"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Consolidate(tc.sel); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	// An override of a rule disable removed changes nothing.
	if _, err := Consolidate(config.GoSelection{Disabled: []string{"HOUSE_GO_GONE"}, Rules: []config.GoRule{withOverride(rule("HOUSE_GO_A", "dedo.yaml"), "HOUSE_GO_GONE")}}); err != nil {
		t.Errorf("override of a disabled rule: %v", err)
	}
}
