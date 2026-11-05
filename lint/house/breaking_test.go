package house_test

import (
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// A breaking house rule runs in buf breaking on each element that the
// earlier version also has, reading that one as against.
func TestBreakingRules(t *testing.T) {
	scopes := config.Rule{ID: "HOUSE_SCOPES_KEPT", On: "method", Check: "against.scopes.all(s, s in scopes)", Message: "keep scopes", Breaking: true, Source: "rules.yml"}
	values := config.Rule{ID: "HOUSE_VALUES_KEPT", On: "enum", Check: "against.values.all(v, v in values)", Message: "keep values", Breaking: true, Source: "rules.yml"}
	specs, err := house.Specs([]config.Rule{scopes, values})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Type != check.RuleTypeBreaking {
			t.Fatalf("%s is a %v rule, want breaking", s.ID, s.Type)
		}
	}
	spec := &check.Spec{Rules: specs, Categories: []*check.CategorySpec{house.CategorySpec}}
	checktest.SpecTest(t, spec)
	deps := []string{"../rules/testdata/deps", "../../proto"}
	request, err := (&checktest.RequestSpec{
		Files:        &checktest.ProtoFileSpec{DirPaths: append([]string{"testdata/breaking/now"}, deps...), FilePaths: []string{"shop.proto"}},
		AgainstFiles: &checktest.ProtoFileSpec{DirPaths: append([]string{"testdata/breaking/against"}, deps...), FilePaths: []string{"shop.proto"}},
	}).ToRequest(t.Context())
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
	got := map[string][]int{}
	for _, a := range response.Annotations() {
		got[a.RuleID()] = append(got[a.RuleID()], a.FileLocation().StartLine()+1)
		if a.AgainstFileLocation() == nil {
			t.Errorf("%s: no location in the earlier version", a.RuleID())
		}
	}
	if lines := got[scopes.ID]; len(lines) != 1 || lines[0] != 10 {
		t.Errorf("%s reported at lines %v, want [10] (GetCart)", scopes.ID, lines)
	}
	if lines := got[values.ID]; len(lines) != 1 {
		t.Errorf("%s reported at lines %v, want the enum once", values.ID, lines)
	}
}

// A check of a breaking rule that reads against compiles only for a
// breaking rule.
func TestAgainstOnlyInBreaking(t *testing.T) {
	r := config.Rule{ID: "HOUSE_X", On: "method", Check: "against.scopes.size() > 0", Message: "x", Source: "rules.yml"}
	if _, err := house.Specs([]config.Rule{r}); err == nil {
		t.Fatal("a lint rule compiled a check that reads against")
	}
}
