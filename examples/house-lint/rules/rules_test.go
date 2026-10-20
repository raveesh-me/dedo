package rules_test

import (
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/examples/house-lint/rules"
)

const deps = "testdata/deps"

func TestSpec(t *testing.T) {
	checktest.SpecTest(t, rules.Spec)
}

func TestResourceTypeDomain(t *testing.T) {
	tests := []struct {
		dir  string
		want []checktest.ExpectedAnnotation
	}{
		{dir: "testdata/pass"},
		{dir: "testdata/fail", want: []checktest.ExpectedAnnotation{{
			RuleID: rules.IDResourceTypeDomain,
			FileLocation: &checktest.ExpectedFileLocation{
				FileName:    "todo.proto",
				StartLine:   7,
				StartColumn: 0,
				EndLine:     15,
				EndColumn:   1,
			},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			checktest.CheckTest{
				Request: &checktest.RequestSpec{
					Files: &checktest.ProtoFileSpec{
						DirPaths:  []string{tt.dir, deps},
						FilePaths: []string{"todo.proto"},
					},
					Options: map[string]any{rules.OptionResourceDomain: "raveesh.me"},
				},
				Spec:                rules.Spec,
				ExpectedAnnotations: tt.want,
			}.Run(t)
		})
	}
}

func TestResourceDomainRequired(t *testing.T) {
	request, err := (&checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{"testdata/pass", deps},
			FilePaths: []string{"todo.proto"},
		},
	}).ToRequest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client, err := check.NewClientForSpec(rules.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Check(t.Context(), request); err == nil {
		t.Fatal("Check without resource_domain succeeded, want an error")
	}
}
