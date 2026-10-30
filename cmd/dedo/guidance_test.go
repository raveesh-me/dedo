package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guidanceRun(t *testing.T, dir string, offline bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	in := &initer{p: &project{dir: dir}, out: &out, offline: offline}
	err := in.guidance()
	return out.String(), err
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dedo's guidance and the annexures dedo.yaml names go in as blocks, after
// the file's own text, annexures last; a second run changes nothing.
func TestGuidanceAnnexure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CLAUDE.md", "# Shop\n\nOur rules.\n")
	writeFile(t, dir, "dedo.yaml", "rules: {}\nannexure:\n  - path: docs/agents.md\n")
	writeFile(t, dir, "docs/agents.md", "# Acme\n\nSend a Version header.\n")
	out, err := guidanceRun(t, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, "CLAUDE.md")
	own := strings.Index(got, "Our rules.")
	dedo := strings.Index(got, beginPrefix+"guidance ")
	annex := strings.Index(got, beginPrefix+"annexure:docs/agents.md ")
	if own < 0 || dedo < own || annex < dedo || !strings.Contains(got, "Send a Version header.\n"+endPrefix+"annexure:docs/agents.md -->") {
		t.Fatalf("CLAUDE.md:\n%s", got)
	}
	if !strings.Contains(out, "added dedo's guidance") || !strings.Contains(out, "added the annexure docs/agents.md") {
		t.Errorf("output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("AGENTS.md created beside CLAUDE.md")
	}
	if out, _ := guidanceRun(t, dir, false); !strings.Contains(out, "up to date") || read(t, dir, "CLAUDE.md") != got {
		t.Errorf("second run changed CLAUDE.md: %s", out)
	}

	// The source changes: the copy follows. Text after the blocks stays.
	writeFile(t, dir, "CLAUDE.md", got+"\n# Later notes\n")
	writeFile(t, dir, "docs/agents.md", "# Acme\n\nSend Version: 2021-07-28.\n")
	out, err = guidanceRun(t, dir, false)
	got = read(t, dir, "CLAUDE.md")
	if err != nil || !strings.Contains(got, "Send Version: 2021-07-28.") || strings.Contains(got, "Send a Version header.") ||
		!strings.HasSuffix(got, "\n# Later notes\n") || !strings.Contains(out, "updated the annexure docs/agents.md") {
		t.Errorf("after the source changed (%v): %s\n%s", err, out, got)
	}

	// dedo.yaml drops it: so does CLAUDE.md.
	writeFile(t, dir, "dedo.yaml", "rules: {}\n")
	out, err = guidanceRun(t, dir, false)
	got = read(t, dir, "CLAUDE.md")
	if err != nil || strings.Contains(got, beginPrefix+annexureKey) || !strings.Contains(out, "removed the annexure docs/agents.md") {
		t.Errorf("after dropping it (%v): %s\n%s", err, out, got)
	}
}

// Guidance an earlier release appended without markers becomes a block, and
// whatever follows it under another top-level heading stays.
func TestGuidanceLegacy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "notes\n\n"+guidanceMarker+"\n\nold text\n\n```sh\n# not a heading\n```\n\n# Ours\n\nkeep me\n")
	out, err := guidanceRun(t, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, "AGENTS.md")
	if strings.Contains(got, "old text") || strings.Count(got, guidanceMarker) != 1 ||
		!strings.HasPrefix(got, "notes\n\n"+beginPrefix+"guidance ") || !strings.HasSuffix(got, endPrefix+"guidance -->\n\n# Ours\n\nkeep me\n") {
		t.Errorf("AGENTS.md:\n%s", got)
	}
	if !strings.Contains(out, "updated dedo's guidance") {
		t.Errorf("output: %s", out)
	}
}

// Offline, a url annexure keeps the copy already there; online, a fetch
// that fails stops dedo rather than dropping the guidance.
func TestGuidanceURLAnnexureOffline(t *testing.T) {
	dir := t.TempDir()
	const url = "https://127.0.0.1:1/AGENTS.md"
	writeFile(t, dir, "dedo.yaml", "annexure:\n  - url: "+url+"\n    sha256: "+strings.Repeat("ab", 32)+"\n")
	kept := block{key: annexureKey + url, text: "# Fetched before\n"}.render()
	writeFile(t, dir, "AGENTS.md", block{key: "guidance", text: "old"}.render()+"\n"+kept)
	out, err := guidanceRun(t, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "AGENTS.md"); !strings.Contains(got, "# Fetched before") || strings.Contains(got, "\nold\n") {
		t.Errorf("AGENTS.md:\n%s", got)
	}
	if !strings.Contains(out, "not fetched offline") {
		t.Errorf("output: %s", out)
	}
	if _, err := guidanceRun(t, dir, false); err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("online: err = %v", err)
	}
}

// A block whose end line was deleted is reported, not guessed at.
func TestGuidanceBrokenBlock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", beginPrefix+"guidance -->\ntext\n")
	if _, err := guidanceRun(t, dir, false); err == nil || !strings.Contains(err.Error(), "no \""+endPrefix+"guidance -->\" line") {
		t.Errorf("err = %v", err)
	}
}
