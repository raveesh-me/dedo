package house_test

import (
	"strings"
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// findings runs house rules over testdata/shop.proto and counts the
// annotations of each rule.
func findings(t *testing.T, rules ...config.Rule) map[string]int {
	t.Helper()
	specs, err := house.Specs(rules)
	if err != nil {
		t.Fatal(err)
	}
	spec := &check.Spec{Rules: specs, Categories: []*check.CategorySpec{house.CategorySpec}}
	checktest.SpecTest(t, spec)
	request, err := (&checktest.RequestSpec{
		Files: &checktest.ProtoFileSpec{
			DirPaths:  []string{"testdata", "testdata/deps", "../rules/testdata/deps", "../../proto"},
			FilePaths: []string{"shop.proto"},
		},
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
	got := map[string]int{}
	for _, a := range response.Annotations() {
		got[a.RuleID()]++
		if strings.Contains(a.Message(), "failed to run") {
			t.Errorf("%s: %s", a.RuleID(), a.Message())
		}
	}
	return got
}

func rule(id, on, check string) config.Rule {
	return config.Rule{ID: id, On: on, Check: check, Message: id + " broken", Source: "rules.yml"}
}

// Each kind of element exposes its variables; a passing check reports
// nothing and a failing one reports each element it fails on.
func TestKinds(t *testing.T) {
	got := findings(t,
		rule("HOUSE_FILE_OK", "file", "proto_package == 'shop.cart.v1' && 'google/api/resource.proto' in imports"),
		rule("HOUSE_FILE_BAD", "file", "name.startsWith('acme/')"),
		rule("HOUSE_SERVICE_OK", "service", "name.endsWith('Service') && 'GetCart' in methods"),
		rule("HOUSE_METHOD_OK", "method", "http_verb == 'GET' && http_path.startsWith('/v1/') && 'shop.cart.read' in scopes && !server_streaming"),
		rule("HOUSE_METHOD_BAD", "method", "http_path.startsWith('/commerce/')"),
		rule("HOUSE_MESSAGE_BAD", "message", "fields.size() > 1"),
		rule("HOUSE_FIELD_BAD", "field", "!name.endsWith('_id')"),
		rule("HOUSE_FIELD_OK", "field", "type == 'string' && (name != 'name' || behaviors.size() > 0)"),
		rule("HOUSE_ENUM_OK", "enum", "values.exists(v, v.endsWith('_UNSPECIFIED'))"),
		rule("HOUSE_RESOURCE_BAD", "resource", "type.endsWith('.acme.com')"),
		rule("HOUSE_RESOURCE_OK", "resource", "patterns == ['carts/{cart}'] && type.split('/')[1] == 'Cart'"),
	)
	want := map[string]int{
		"HOUSE_FILE_BAD":     1,
		"HOUSE_METHOD_BAD":   1,
		"HOUSE_MESSAGE_BAD":  1, // GetCartRequest has one field
		"HOUSE_FIELD_BAD":    1, // owner_id
		"HOUSE_RESOURCE_BAD": 1,
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("%s reported %d times, want %d", id, got[id], n)
		}
	}
	for id, n := range got {
		if want[id] == 0 {
			t.Errorf("%s reported %d times, want none", id, n)
		}
	}
}

func TestSpecsRejectBadChecks(t *testing.T) {
	for name, check := range map[string]string{
		"syntax":   "name.endsWith(",
		"unknown":  "colour == 'red'",
		"not bool": "name",
		"types":    "repeated == 'yes'",
	} {
		_, err := house.Specs([]config.Rule{rule("HOUSE_X", "field", check)})
		if err == nil || !strings.Contains(err.Error(), "HOUSE_X") || !strings.Contains(err.Error(), "rules.yml") {
			t.Errorf("%s: err = %v, want it to name the rule and its file", name, err)
		}
	}
}

func TestMessageCarriesDoc(t *testing.T) {
	r := rule("HOUSE_DOC", "resource", "false")
	r.Doc = "https://wiki.example.com/resources"
	specs, err := house.Specs([]config.Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Purpose != "HOUSE_DOC broken." || specs[0].CategoryIDs[0] != house.Category {
		t.Errorf("spec = %+v", specs[0])
	}
}
