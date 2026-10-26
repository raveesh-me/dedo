package rules

import (
	"testing"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// A plugin built with house rules runs those, without a rules.yml to read.
func TestEmbeddedHouseRules(t *testing.T) {
	s, err := config.Encode([]config.Rule{{
		ID: "HOUSE_BUILT_IN", On: "field", Check: "true", Message: "x",
		Overrides: []string{"DEDO_NO_JSON_NAME"}, Source: "rules.yml", File: "/r/rules.yml",
	}}, nil)
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
	embedded = "garbage!"
	if _, err := Load(); err == nil {
		t.Error("a corrupt embedded value was accepted")
	}
}
