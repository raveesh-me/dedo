package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// A key dedo doesn't know is either a typo or written for a newer dedo; the
// error says which file and line, and what to do about each.
func TestUnknownKeyNamesTheFix(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  []string
	}{
		"own file, section": {
			map[string]string{FileName: "lnit:\n  use: [DEDO]\n"},
			[]string{FileName + ":1:", "`lnit`", "the top of the file", "dedo update && dedo upgrade"},
		},
		"imported file, section": {
			map[string]string{FileName: "imports:\n  - path: shared.yml\n", "shared.yml": domainRule + "metrics:\n  rules: []\n"},
			[]string{"shared.yml:7:", "`metrics`", "the top of the file", "newer dedo", "dedo update && dedo upgrade"},
		},
		"rule field": {
			map[string]string{FileName: "lint:\n  rules:\n  - {id: HOUSE_X, on: field, check: \"true\", message: x, severity: high}\n"},
			[]string{FileName + ":3:", "`severity`", "a rule"},
		},
		"go rule field": {
			map[string]string{FileName: "go:\n  rules:\n  - {id: HOUSE_GO_X, forbid_import: example.com/x, message: x, when: test}\n"},
			[]string{FileName + ":3:", "`when`", "a go rule"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.files)
			_, err := Load(filepath.Join(dir, FileName))
			if err == nil {
				t.Fatal("Load succeeded")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err = %v\nwant it to mention %q", err, w)
				}
			}
			if strings.Contains(err.Error(), "config.") {
				t.Errorf("err names a Go type: %v", err)
			}
		})
	}
}

// An annexure is known to dedo, but only in dedo.yaml.
func TestAnnexureInRulesFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		FileName:     "imports:\n  - path: shared.yml\n",
		"shared.yml": "annexure:\n  - path: guide.md\n",
	})
	_, err := Load(filepath.Join(dir, FileName))
	if err == nil || !strings.Contains(err.Error(), "annexure belongs in the repository's own dedo.yaml") {
		t.Errorf("err = %v", err)
	}
}

func TestDedoVersion(t *testing.T) {
	// go test builds a (devel) main module, which names no release.
	if v := dedoVersion(); v != "" {
		t.Errorf("dedoVersion() = %q under go test", v)
	}
}
