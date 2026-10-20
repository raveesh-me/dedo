package aip_test

import (
	"slices"
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"
	"github.com/googleapis/api-linter/v2/lint"

	"github.com/raveesh-me/dedo/lint/aip"
)

func spec(t *testing.T) *check.Spec {
	t.Helper()
	rules, categories, before, err := aip.Rules()
	if err != nil {
		t.Fatal(err)
	}
	return &check.Spec{Rules: rules, Categories: categories, Before: before}
}

func TestSpec(t *testing.T) {
	checktest.SpecTest(t, spec(t))
}

func TestRuleID(t *testing.T) {
	tests := map[string]string{
		"core::0131::http-method":                 "AIP_0131_HTTP_METHOD",
		"core::0191::java-package":                "AIP_0191_JAVA_PACKAGE",
		"client-libraries::4232::repeated-fields": "AIP_4232_REPEATED_FIELDS",
	}
	for name, want := range tests {
		if got := aip.RuleID(lint.RuleName(name)); got != want {
			t.Errorf("RuleID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCategories(t *testing.T) {
	tests := []struct {
		id      string
		want    []string
		notWant []string
	}{
		{"AIP_0131_HTTP_METHOD", []string{"AIP", "AIP_0131", "AIP_CORE", "AIP_CRUD", "AIP_NOLANG", "AIP_RECOMMENDED"}, nil},
		{"AIP_0191_JAVA_PACKAGE", []string{"AIP", "AIP_0191", "AIP_CORE"}, []string{"AIP_NOLANG"}},
		{"AIP_0192_HAS_COMMENTS", []string{"AIP_NOLANG"}, []string{"AIP_RECOMMENDED"}},
	}
	byID := map[string]*check.RuleSpec{}
	for _, r := range spec(t).Rules {
		byID[r.ID] = r
	}
	for _, tt := range tests {
		r, ok := byID[tt.id]
		if !ok {
			t.Errorf("rule %s missing", tt.id)
			continue
		}
		for _, c := range tt.want {
			if !slices.Contains(r.CategoryIDs, c) {
				t.Errorf("%s: categories %v lack %s", tt.id, r.CategoryIDs, c)
			}
		}
		for _, c := range tt.notWant {
			if slices.Contains(r.CategoryIDs, c) {
				t.Errorf("%s: categories %v include %s", tt.id, r.CategoryIDs, c)
			}
		}
	}
}

// TestReports runs the linter over a fixture whose GetBook uses POST.
func TestReports(t *testing.T) {
	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../rules/testdata/deps"},
				FilePaths: []string{"library.proto"},
			},
			RuleIDs: []string{"AIP_0131_HTTP_METHOD"},
		},
		Spec: spec(t),
		ExpectedAnnotations: []checktest.ExpectedAnnotation{{
			RuleID: "AIP_0131_HTTP_METHOD",
			FileLocation: &checktest.ExpectedFileLocation{
				FileName:    "library.proto",
				StartLine:   10,
				StartColumn: 4,
				EndLine:     13,
				EndColumn:   6,
			},
		}},
	}.Run(t)
}
