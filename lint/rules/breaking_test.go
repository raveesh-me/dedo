package rules_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/lint/house/config"
	"github.com/raveesh-me/dedo/lint/rules"
)

// TestBreakingRules runs every breaking rule against
// testdata/breaking/<rule>: pass and fail are each compared with against.
// Pass must produce no annotation; fail one on each line marked "// want".
func TestBreakingRules(t *testing.T) {
	t.Setenv(config.EnvFile, filepath.Join("testdata", config.FileName))
	for _, rule := range rules.Breaking {
		if rule.Type != check.RuleTypeBreaking {
			t.Fatalf("%s is in Breaking but is a %v rule", rule.ID, rule.Type)
		}
		_, name, _ := strings.Cut(rule.ID, "_")
		dir := filepath.Join("testdata", "breaking", strings.ToLower(name))
		t.Run(rule.ID, func(t *testing.T) {
			against := filepath.Join(dir, "against")
			if got := breakingLines(t, rule.ID, filepath.Join(dir, "pass"), against); len(got) != 0 {
				t.Fatalf("pass: unexpected annotations: %v", got)
			}
			fail := filepath.Join(dir, "fail")
			got := breakingLines(t, rule.ID, fail, against)
			want := markedLines(t, fail)
			if len(want) == 0 {
				t.Fatal("fail fixture marks no lines")
			}
			if !slices.Equal(got, want) {
				t.Fatalf("annotated lines %v, want %v", got, want)
			}
		})
	}
}

func breakingLines(t *testing.T, ruleID, dir, against string) []string {
	t.Helper()
	deps := []string{"testdata/deps", "../../proto"}
	request, err := (&checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  append([]string{dir}, deps...),
			FilePaths: protoFiles(t, dir),
		},
		AgainstFiles: &checktest.ProtoFileSpec{
			DirPaths:  append([]string{against}, deps...),
			FilePaths: protoFiles(t, against),
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
