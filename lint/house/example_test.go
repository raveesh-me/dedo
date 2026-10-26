package house_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// The example rules.yml has only keys a rules file takes, and every rule
// in it compiles.
func TestExampleRulesFile(t *testing.T) {
	f, err := os.Open("../../templates/rules.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var file struct {
		Use     []string        `yaml:"use"`
		Imports []config.Import `yaml:"imports"`
		Disable []string        `yaml:"disable"`
		Rules   []config.Rule   `yaml:"rules"`
	}
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		t.Fatal(err)
	}
	if strings.Join(file.Use, ",") != "AIP_NOLANG,ZALANDO,DEDO,HOUSE,DEDO_MULTI_TENANT" {
		t.Errorf("use = %v, want all five rule sets", file.Use)
	}
	if len(file.Imports) < 3 || len(file.Disable) == 0 {
		t.Errorf("imports %v, disable %v: the example should show each", file.Imports, file.Disable)
	}
	if _, err := house.Specs(file.Rules); err != nil {
		t.Fatal(err)
	}
}
