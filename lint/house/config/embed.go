package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// embedded is the form house rules take inside a lint plugin built with
// them, for the WebAssembly plugin, which cannot read rules.yml: the rules
// as Load returned them, with what ranks them, and the IDs disable removed.
type embedded struct {
	Rules    []Rule   `json:"rules"`
	Disabled []string `json:"disabled"`
}

// Encode returns house rules, as LoadAll returned them, in a form to build
// into a lint plugin: base64, so it passes through go build -ldflags -X.
func Encode(rules []Rule, disabled []string) (string, error) {
	b, err := json.Marshal(embedded{Rules: rules, Disabled: disabled})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Decode returns the house rules and disabled IDs Encode encoded.
func Decode(s string) ([]Rule, []string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, nil, fmt.Errorf("embedded house rules: %w", err)
	}
	var e embedded
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, nil, fmt.Errorf("embedded house rules: %w", err)
	}
	return e.Rules, e.Disabled, nil
}
