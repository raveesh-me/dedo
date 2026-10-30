package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Encode returns the house rules, as Load returned them, in a form to build into
// a lint plugin, for the WebAssembly plugin, which cannot read dedo.yaml:
// JSON in base64, so it passes through go build -ldflags -X.
func Encode(c Config) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Decode returns the Config Encode encoded.
func Decode(s string) (Config, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return Config{}, fmt.Errorf("embedded house rules: %w", err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("embedded house rules: %w", err)
	}
	return c, nil
}
