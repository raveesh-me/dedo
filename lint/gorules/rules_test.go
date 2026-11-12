package gorules

import (
	"os"
	"strings"
	"testing"
)

// Every rule a finding links to has its section in RULES.md.
func TestRulesDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../guidance/RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range Builtins() {
		if !strings.Contains(string(doc), "\n### "+b.ID+"\n") {
			t.Errorf("RULES.md has no section for %s", b.ID)
		}
		if b.Analyzer.Name != analyzerName(b.ID) {
			t.Errorf("%s: analyzer %s", b.ID, b.Analyzer.Name)
		}
	}
}
