package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// House rules survive Encode and Decode whole, with what ranks them.
func TestEncodeDecode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"shared.yml": domainRule + "  - {id: HOUSE_GONE, on: field, check: \"true\", message: x}\n",
		FileName:     "imports:\n  - path: shared.yml\ndisable: [HOUSE_GONE]\nrules:\n  - {id: HOUSE_OWN, on: field, check: \"true\", message: x, overrides: [HOUSE_RESOURCE_TYPE_DOMAIN], final: true}\n",
	})
	rules, disabled, err := LoadAll(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	s, err := Encode(rules, disabled)
	if err != nil {
		t.Fatal(err)
	}
	gotRules, gotDisabled, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotRules, rules) || !reflect.DeepEqual(gotDisabled, disabled) {
		t.Errorf("decoded %+v %v, want %+v %v", gotRules, gotDisabled, rules, disabled)
	}
	if _, _, err := Decode("not base64!"); err == nil {
		t.Error("Decode accepted garbage")
	}
}
