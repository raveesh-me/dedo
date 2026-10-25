package rules_test

import (
	"strings"
	"testing"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/aip"
	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
	"github.com/raveesh-me/dedo/lint/rules"
)

func builtin(t *testing.T) []*check.RuleSpec {
	t.Helper()
	aipRules, _, _, err := aip.Rules()
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]*check.RuleSpec{}, rules.All...), aipRules...)
}

// houseRule is a rule of the file named file, which the files in importers
// import.
func houseRule(id, file string, overrides []string, importers ...string) config.Rule {
	return config.Rule{
		ID: id, On: "method", Check: "true", Message: id, Overrides: overrides,
		Source: file, File: "/repo/" + file, Importers: prefixed(importers),
	}
}

func prefixed(files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, "/repo/"+f)
	}
	return out
}

func consolidate(t *testing.T, houseRules ...config.Rule) (*rules.Consolidated, error) {
	t.Helper()
	specs, err := house.Specs(houseRules)
	if err != nil {
		t.Fatal(err)
	}
	return rules.Consolidate(builtin(t), houseRules, specs, []string{"HOUSE_DISABLED"})
}

// overriddenBy returns the rules each overridden rule is overridden by, as
// its purpose names them.
func overriddenBy(c *rules.Consolidated) map[string]string {
	out := map[string]string{}
	for _, r := range c.Rules {
		if by, ok := strings.CutPrefix(r.Purpose, "Overridden by "); ok {
			out[r.ID], _, _ = strings.Cut(by, ".")
		}
	}
	return out
}

func TestBuiltinOverrides(t *testing.T) {
	c, err := consolidate(t)
	if err != nil {
		t.Fatal(err)
	}
	got := overriddenBy(c)
	for _, id := range []string{"AIP_0131_RESPONSE_MESSAGE_NAME", "AIP_0135_RESPONSE_MESSAGE_NAME", "AIP_0134_REQUEST_REQUIRED_FIELDS"} {
		if got[id] != "DEDO_HTTP_RESPONSE_BODY" {
			t.Errorf("%s overridden by %q, want DEDO_HTTP_RESPONSE_BODY", id, got[id])
		}
	}
	if got["AIP_0131_HTTP_METHOD"] != "" || c.Overridden != 7 {
		t.Errorf("overridden = %v", got)
	}
}

// House rules outrank dedo's, and a rules file outranks the files it
// imports; an overridden rule's own overrides lapse.
func TestHouseOverrides(t *testing.T) {
	c, err := consolidate(t,
		houseRule("HOUSE_SHARED", "shared.yml", []string{"DEDO_HTTP_PATH_VERSION"}, "rules.yml"),
		houseRule("HOUSE_BARE_RESOURCES", "shared.yml", []string{"DEDO_HTTP_RESPONSE_BODY"}, "rules.yml"),
		houseRule("HOUSE_OWN", "rules.yml", []string{"HOUSE_SHARED"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	got := overriddenBy(c)
	want := map[string]string{
		"HOUSE_SHARED":            "HOUSE_OWN",
		"DEDO_HTTP_RESPONSE_BODY": "HOUSE_BARE_RESOURCES",
	}
	for id, by := range want {
		if got[id] != by {
			t.Errorf("%s overridden by %q, want %q", id, got[id], by)
		}
	}
	// HOUSE_SHARED is overridden, so DEDO_HTTP_PATH_VERSION runs again;
	// so do the AIP rules DEDO_HTTP_RESPONSE_BODY overrode.
	for _, id := range []string{"DEDO_HTTP_PATH_VERSION", "AIP_0131_RESPONSE_MESSAGE_NAME"} {
		if got[id] != "" {
			t.Errorf("%s overridden by %q, want it to run", id, got[id])
		}
	}
	report := c.Report()
	for _, line := range []string{
		"HOUSE_OWN (rules.yml) overrides HOUSE_SHARED\n",
		"HOUSE_SHARED (shared.yml) overrides DEDO_HTTP_PATH_VERSION, lapsed",
		"DEDO_HTTP_RESPONSE_BODY overrides AIP_0131_RESPONSE_MESSAGE_NAME, lapsed",
	} {
		if !strings.Contains(report, line) {
			t.Errorf("report lacks %q:\n%s", line, report)
		}
	}
}

func TestOverridesRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		rules []config.Rule
		want  []string
	}{
		"final": {
			[]config.Rule{houseRule("HOUSE_X", "rules.yml", []string{"DEDO_METHOD_RULES_REQUIRED"})},
			[]string{"rules.yml: HOUSE_X overrides DEDO_METHOD_RULES_REQUIRED, which is final", "policy.Build refuses a method without scopes"},
		},
		"unknown": {
			[]config.Rule{houseRule("HOUSE_X", "rules.yml", []string{"AIP_9999_NOPE"})},
			[]string{"HOUSE_X overrides AIP_9999_NOPE, which no rule defines"},
		},
		"house final": {
			[]config.Rule{
				func() config.Rule {
					r := houseRule("HOUSE_LOCKED", "shared.yml", nil, "rules.yml")
					r.Final = true
					return r
				}(),
				houseRule("HOUSE_OWN", "rules.yml", []string{"HOUSE_LOCKED"}),
			},
			[]string{"rules.yml: HOUSE_OWN overrides HOUSE_LOCKED, which shared.yml marks final"},
		},
		"importer": {
			[]config.Rule{
				houseRule("HOUSE_SHARED", "shared.yml", []string{"HOUSE_OWN"}, "rules.yml"),
				houseRule("HOUSE_OWN", "rules.yml", nil),
			},
			[]string{"shared.yml: HOUSE_SHARED cannot override HOUSE_OWN (rules.yml)", "files its rules file imports"},
		},
		"same file": {
			[]config.Rule{
				houseRule("HOUSE_A", "rules.yml", []string{"HOUSE_B"}),
				houseRule("HOUSE_B", "rules.yml", nil),
			},
			[]string{"HOUSE_A cannot override HOUSE_B (rules.yml)", "not rules of its own file"},
		},
		"sibling": {
			[]config.Rule{
				houseRule("HOUSE_A", "a.yml", []string{"HOUSE_B"}, "rules.yml"),
				houseRule("HOUSE_B", "b.yml", nil, "rules.yml"),
			},
			[]string{"HOUSE_A cannot override HOUSE_B (b.yml)"},
		},
		"all at once": {
			[]config.Rule{
				houseRule("HOUSE_X", "rules.yml", []string{"ZALANDO_SERVICE_RULES_REQUIRED"}),
				houseRule("HOUSE_Y", "rules.yml", []string{"HOUSE_NOPE"}),
			},
			[]string{"HOUSE_X overrides ZALANDO_SERVICE_RULES_REQUIRED, which is final", "HOUSE_Y overrides HOUSE_NOPE"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := consolidate(t, tc.rules...)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %v, want it to mention %q", err, w)
				}
			}
		})
	}
}

// A repository can import several house sources side by side. Its own
// rules may override any of them, each source may override dedo's rules,
// and an override of a rule rules.yml disables changes nothing.
func TestSeveralHouseSources(t *testing.T) {
	c, err := consolidate(t,
		houseRule("HOUSE_ORG_ROOT", "org.yml", []string{"DEDO_HTTP_PATH_VERSION"}, "rules.yml"),
		houseRule("HOUSE_TEAM_TYPE", "team.yml", []string{"DEDO_NO_JSON_NAME"}, "rules.yml"),
		houseRule("HOUSE_OWN_ROOT", "rules.yml", []string{"HOUSE_ORG_ROOT", "HOUSE_TEAM_TYPE", "HOUSE_DISABLED"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	got := overriddenBy(c)
	want := map[string]string{
		"HOUSE_ORG_ROOT":    "HOUSE_OWN_ROOT",
		"HOUSE_TEAM_TYPE":   "HOUSE_OWN_ROOT",
		"DEDO_NO_JSON_NAME": "",
	}
	for id, by := range want {
		if got[id] != by {
			t.Errorf("%s overridden by %q, want %q", id, got[id], by)
		}
	}
}

// The report lists every final rule, dedo's and the house's.
func TestReportListsFinalRules(t *testing.T) {
	r := houseRule("HOUSE_LOCKED", "shared.yml", []string{"DEDO_NO_JSON_NAME"}, "rules.yml")
	r.Final = true
	c, err := consolidate(t, r)
	if err != nil {
		t.Fatal(err)
	}
	report := c.Report()
	for _, line := range []string{"  DEDO_METHOD_RULES_REQUIRED is final\n", "  HOUSE_LOCKED (shared.yml) is final\n", "  HOUSE_LOCKED (shared.yml) overrides DEDO_NO_JSON_NAME\n"} {
		if !strings.Contains(report, line) {
			t.Errorf("report lacks %q:\n%s", line, report)
		}
	}
}

// Every final rule exists, so the list cannot rot.
func TestFinalRulesExist(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range rules.All {
		ids[r.ID] = true
	}
	for id := range rules.Final {
		if !ids[id] {
			t.Errorf("final rule %s does not exist", id)
		}
	}
}
