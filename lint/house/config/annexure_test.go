package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnexures(t *testing.T) {
	const guide = "# Acme agents\n\nSend Version.\n"
	base := serve(t, map[string]string{"/v1/AGENTS.md": guide})
	dir := t.TempDir()
	write(t, dir, map[string]string{
		FileName: "rules: {}\nannexure:\n  - url: " + base + "/v1/AGENTS.md\n    sha256: " + sum(guide) +
			"\n  - path: docs/agents.md\n",
		"docs/agents.md": "# Local\n",
	})
	got, err := Annexures(filepath.Join(dir, FileName))
	if err != nil || len(got) != 2 || got[0].Name() != base+"/v1/AGENTS.md" || got[1].Name() != "docs/agents.md" {
		t.Fatalf("Annexures = %+v, %v", got, err)
	}
	for i, want := range []string{guide, "# Local\n"} {
		if b, err := ReadAnnexure(dir, got[i]); err != nil || string(b) != want {
			t.Errorf("ReadAnnexure(%s) = %q, %v", got[i].Name(), b, err)
		}
	}

	// Unpinned, the fetch names the sum to pin; pinned wrong, it fails.
	if _, err := ReadAnnexure(dir, Annexure{URL: base + "/v1/AGENTS.md"}); err == nil || !strings.Contains(err.Error(), sum(guide)) {
		t.Errorf("unpinned: err = %v", err)
	}
	if _, err := ReadAnnexure(dir, Annexure{URL: base + "/v1/AGENTS.md", SHA256: sum("other")}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("wrong pin: err = %v", err)
	}
	if _, err := ReadAnnexure(dir, Annexure{Path: "docs/missing.md"}); err == nil {
		t.Error("missing path: no error")
	}

	// A rules file has no annexures.
	write(t, dir, map[string]string{RulesFileName: "rules: []\n"})
	if got, err := Annexures(filepath.Join(dir, RulesFileName)); err != nil || got != nil {
		t.Errorf("rules.yml: %v, %v", got, err)
	}
}

func TestAnnexuresReject(t *testing.T) {
	for name, tc := range map[string]struct{ annexure, want string }{
		"neither":       {"  - {}\n", "one of them"},
		"both":          {"  - {path: a.md, url: 'https://x/a.md'}\n", "one of them"},
		"http":          {"  - url: http://x/a.md\n", "https"},
		"path pinned":   {"  - {path: a.md, sha256: abc}\n", "only a url"},
		"malformed pin": {"  - {url: 'https://x/a.md', sha256: abc}\n", "64 hex"},
		"unknown key":   {"  - {file: a.md}\n", "file"},
	} {
		dir := t.TempDir()
		write(t, dir, map[string]string{FileName: "annexure:\n" + tc.annexure})
		if _, err := Annexures(filepath.Join(dir, FileName)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}
