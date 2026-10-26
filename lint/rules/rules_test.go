package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/lint/house/config"
	"github.com/raveesh-me/dedo/lint/rules"
)

// wantMarker marks a fixture line where the rule must report an annotation.
const wantMarker = "// want"

func TestSpec(t *testing.T) {
	spec, err := rules.NewSpec()
	if err != nil {
		t.Fatal(err)
	}
	checktest.SpecTest(t, spec)
}

// TestRules runs every rule against testdata/<rule>/pass and
// testdata/<rule>/fail. Pass fixtures must produce no annotation. Fail
// fixtures must produce one annotation on each line marked "// want".
func TestRules(t *testing.T) {
	t.Setenv(config.EnvFile, filepath.Join("testdata", config.FileName))
	for _, rule := range rules.All {
		_, name, _ := strings.Cut(rule.ID, "_")
		dir := filepath.Join("testdata", strings.ToLower(name))
		t.Run(rule.ID, func(t *testing.T) {
			t.Run("pass", func(t *testing.T) {
				got := annotationLines(t, rule.ID, filepath.Join(dir, "pass"))
				if len(got) != 0 {
					t.Fatalf("unexpected annotations: %v", got)
				}
			})
			t.Run("fail", func(t *testing.T) {
				fixture := filepath.Join(dir, "fail")
				got := annotationLines(t, rule.ID, fixture)
				want := markedLines(t, fixture)
				if len(want) == 0 {
					t.Fatal("fail fixture marks no lines")
				}
				if !slices.Equal(got, want) {
					t.Fatalf("annotated lines %v, want %v", got, want)
				}
			})
		})
	}
}

// annotationLines runs one rule over a fixture directory and returns the
// file:line of each annotation, one per distinct line, sorted.
func annotationLines(t *testing.T, ruleID, dir string) []string {
	t.Helper()
	files := protoFiles(t, dir)
	request, err := (&checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{dir, "testdata/deps", "../../proto"},
			FilePaths: files,
		},
		RuleIDs: []string{ruleID},
	}).ToRequest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spec, err := rules.NewSpec()
	if err != nil {
		t.Fatal(err)
	}
	client, err := check.NewClientForSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Check(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range response.Annotations() {
		if link := " (" + rules.DocsURL(a.RuleID()) + ")"; !strings.HasSuffix(a.Message(), link) {
			t.Errorf("message %q does not end with its docs link%s", a.Message(), link)
		}
		loc := a.FileLocation()
		if loc == nil {
			t.Fatalf("annotation without location: %s", a.Message())
		}
		lines = append(lines, location(loc.FileDescriptor().ProtoreflectFileDescriptor().Path(), loc.StartLine()))
	}
	slices.Sort(lines)
	return slices.Compact(lines)
}

func markedLines(t *testing.T, dir string) []string {
	t.Helper()
	var lines []string
	for _, name := range protoFiles(t, dir) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, wantMarker) {
				lines = append(lines, location(name, i))
			}
		}
	}
	slices.Sort(lines)
	return lines
}

func protoFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".proto") {
			files = append(files, e.Name())
		}
	}
	return files
}

func location(file string, line int) string {
	// Zero-padded so lexical order matches line order.
	return fmt.Sprintf("%s:%04d", file, line)
}

// TestRulesDocumented keeps RULES.md in step with the rules: every
// rule's docs link must land on a heading, and every heading on a rule.
func TestRulesDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../guidance/RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]bool{}
	for _, line := range strings.Split(string(doc), "\n") {
		if id, ok := strings.CutPrefix(line, "### "); ok {
			headings[id] = true
		}
	}
	for _, rule := range rules.All {
		if !headings[rule.ID] {
			t.Errorf("RULES.md has no \"### %s\" heading", rule.ID)
		}
		if want := "/guidance/RULES.md#" + strings.ToLower(rule.ID); !strings.HasSuffix(rules.DocsURL(rule.ID), want) {
			t.Errorf("DocsURL(%s) = %s", rule.ID, rules.DocsURL(rule.ID))
		}
		delete(headings, rule.ID)
	}
	for id := range headings {
		t.Errorf("RULES.md documents %q, which is not a rule", id)
	}
}
