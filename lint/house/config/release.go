package config

import (
	"fmt"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// running is the release of dedo reading the rules; tests replace it.
var running = dedoVersion

// checkRelease enforces a file's dedo key, the oldest dedo release that
// reads it, as go.mod's go line names the oldest Go. It runs before the file
// is decoded, so an older dedo says to upgrade rather than reporting a key it
// doesn't know or a check it cannot compile. A development build, whose
// release is unknown, reads any file.
func checkRelease(path string, b []byte) error {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(b, &top); err != nil {
		return nil // decode reports it
	}
	n, ok := top["dedo"]
	if !ok {
		return nil
	}
	want := n.Value
	if n.Kind != yaml.ScalarNode || !semver.IsValid(want) || semver.Canonical(want) != want || semver.Prerelease(want) != "" {
		return fmt.Errorf("%s:%d: dedo names %q, which is not a release such as v0.38.0: the oldest dedo release that reads this file", path, n.Line, want)
	}
	have := running()
	if have == "" || module.IsPseudoVersion(have) {
		return nil
	}
	if semver.Compare(have, want) < 0 {
		return fmt.Errorf("%s: needs dedo %s or later; this is dedo %s: run `dedo update && dedo upgrade`", path, want, have)
	}
	return nil
}
