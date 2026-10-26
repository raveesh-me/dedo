package rules

import (
	"slices"
	"testing"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// A plugin built with a rules.yml runs its rules, without the file to read.
func TestEmbeddedHouseRules(t *testing.T) {
	s, err := config.Encode(config.Config{
		Use: []string{"AIP_NOLANG", "DEDO"},
		Rules: []config.Rule{{
			ID: "HOUSE_BUILT_IN", On: "field", Check: "true", Message: "x",
			Overrides: []string{"DEDO_NO_JSON_NAME"}, Source: "rules.yml", File: "/r/rules.yml",
		}},
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
	if c.House != 1 || c.Overridden != 8 {
		t.Errorf("house %d, overridden %d; want 1 and 8 (dedo's 7 and DEDO_NO_JSON_NAME)", c.House, c.Overridden)
	}
	if !slices.Equal(c.Use, []string{"AIP_NOLANG", "DEDO"}) {
		t.Errorf("use %v, want the built-in rules.yml's", c.Use)
	}
	embedded = "garbage!"
	if _, err := Load(); err == nil {
		t.Error("a corrupt embedded value was accepted")
	}
}
