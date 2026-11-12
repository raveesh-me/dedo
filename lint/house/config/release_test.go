package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// A rules file names the oldest dedo release that reads it, as go.mod's go
// line names the oldest Go; an older dedo says to upgrade before anything
// else, a newer key or a check it cannot compile included.
func TestRequiredRelease(t *testing.T) {
	const shared = "dedo: v0.39.0\nlint:\n  rules:\n  - {id: HOUSE_X, on: field, check: newer_variable, message: x}\n"
	for name, tc := range map[string]struct {
		running string
		file    string
		want    string // "" means Load succeeds
	}{
		"older release":     {"v0.38.0", shared, "needs dedo v0.39.0 or later; this is dedo v0.38.0"},
		"older patch":       {"v0.39.0", "dedo: v0.39.1\n", "needs dedo v0.39.1 or later"},
		"same release":      {"v0.39.0", "dedo: v0.39.0\n", ""},
		"newer release":     {"v1.2.0", "dedo: v0.39.0\n", ""},
		"development build": {"", "dedo: v9.0.0\n", ""},
		"pseudo-version":    {"v0.38.1-0.20261112093000-abcdef123456", "dedo: v9.0.0\n", ""},
		"not a version":     {"v0.38.0", "dedo: latest\n", "dedo names \"latest\", which is not a release such as v0.38.0"},
		"partial version":   {"v0.38.0", "dedo: v0.38\n", "not a release such as v0.38.0"},
		"prerelease":        {"v0.38.0", "dedo: v0.39.0-rc.1\n", "not a release such as v0.38.0"},
		"not a string":      {"v0.38.0", "dedo: {min: v0.38.0}\n", "not a release such as v0.38.0"},
	} {
		t.Run(name, func(t *testing.T) {
			old := running
			running = func() string { return tc.running }
			t.Cleanup(func() { running = old })
			dir := t.TempDir()
			write(t, dir, map[string]string{FileName: "imports:\n  - path: shared.yml\n", "shared.yml": tc.file})
			_, err := Load(filepath.Join(dir, FileName))
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Load: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("err = %v\nwant it to mention %q", err, tc.want)
			case tc.want != "" && strings.HasPrefix(tc.want, "needs") && !strings.Contains(err.Error(), "shared.yml: needs"):
				t.Errorf("err = %v, want it to name shared.yml", err)
			}
		})
	}
}

// The service's own dedo.yaml may name a release too.
func TestRequiredReleaseInDedoYAML(t *testing.T) {
	old := running
	running = func() string { return "v0.38.0" }
	t.Cleanup(func() { running = old })
	dir := t.TempDir()
	write(t, dir, map[string]string{FileName: "dedo: v0.40.0\n"})
	if _, err := Load(filepath.Join(dir, FileName)); err == nil || !strings.Contains(err.Error(), "dedo update && dedo upgrade") {
		t.Errorf("err = %v", err)
	}
}
