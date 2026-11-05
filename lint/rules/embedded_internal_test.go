package rules

import (
	"slices"
	"testing"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// A plugin built with a rules.yml runs its rules, without the file to read.
func TestEmbeddedHouseRules(t *testing.T) {
	s, err := config.Encode(config.Config{
		Lint: config.Selection{
			Use: []string{"AIP_NOLANG", "DEDO", "HOUSE"},
			Rules: []config.Rule{{
				ID: "HOUSE_BUILT_IN", On: "field", Check: "true", Message: "x",
				Overrides: []string{"DEDO_NO_JSON_NAME"}, Source: "rules.yml", File: "/r/rules.yml",
			}},
		},
		Breaking: config.Selection{
			Rules: []config.Rule{{
				ID: "HOUSE_BUILT_IN_BREAKING", On: "method", Check: "against.scopes.all(s, s in scopes)", Message: "k",
				Overrides: []string{"DEDO_SCOPES_NO_DELETE"}, Breaking: true, Source: "rules.yml", File: "/r/rules.yml",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	embedded = s
	t.Cleanup(func() { embedded = "" })
	t.Chdir(t.TempDir()) // no rules.yml here
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Lint.House != 1 || c.Lint.Overridden != 8 {
		t.Errorf("house %d, overridden %d; want 1 and 8 (dedo's 7 and DEDO_NO_JSON_NAME)", c.Lint.House, c.Lint.Overridden)
	}
	if !slices.Equal(c.Lint.Use, []string{"AIP_NOLANG", "DEDO", "HOUSE"}) {
		t.Errorf("use %v, want the built-in rules.yml's", c.Lint.Use)
	}
	if c.Breaking.House != 1 || c.Breaking.Overridden != 1 || !c.Breaking.DefaultUse {
		t.Errorf("breaking: house %d, overridden %d, default use %v; want 1, 1 (DEDO_SCOPES_NO_DELETE), true", c.Breaking.House, c.Breaking.Overridden, c.Breaking.DefaultUse)
	}
	embedded = "garbage!"
	if _, err := Load(); err == nil {
		t.Error("a corrupt embedded value was accepted")
	}
}
