package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// A rules.yml survives Encode and Decode whole, with what ranks its rules.
func TestEncodeDecode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"shared.yml":  domainRule + "  - {id: HOUSE_GONE, on: field, check: \"true\", message: x}\n",
		RulesFileName: "use: [DEDO]\nimports:\n  - path: shared.yml\ndisable: [HOUSE_GONE, DEDO_NO_JSON_NAME]\nrules:\n  - {id: HOUSE_OWN, on: field, check: \"true\", message: x, overrides: [HOUSE_RESOURCE_TYPE_DOMAIN], final: true}\n",
	})
	c, err := Load(filepath.Join(dir, RulesFileName))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("decoded %+v, want %+v", got, c)
	}
	if _, err := Decode("not base64!"); err == nil {
		t.Error("Decode accepted garbage")
	}
}
