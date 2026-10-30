package house_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// The example dedo.yaml has only keys dedo.yaml takes, every rule in it
// compiles, and its annexures are well formed.
func TestExampleRulesFile(t *testing.T) {
	f, err := os.Open("../../templates/dedo.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var doc struct {
		Rules struct {
			Use     []string        `yaml:"use"`
			Imports []config.Import `yaml:"imports"`
			Disable []string        `yaml:"disable"`
			Rules   []config.Rule   `yaml:"rules"`
		} `yaml:"rules"`
		Annexure []config.Annexure `yaml:"annexure"`
	}
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	file := doc.Rules
	b, err := os.ReadFile("../../templates/dedo.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	asDedo := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(asDedo, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := config.Annexures(asDedo); err != nil || len(got) != 2 {
		t.Errorf("annexure %v, %v: the example should show a url and a path", got, err)
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
