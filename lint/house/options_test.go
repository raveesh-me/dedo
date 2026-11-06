package house_test

import (
	"strings"
	"testing"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checktest"

	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// optionFindings runs house rules over testdata/options, whose options come
// from acme/auth/v1/options.proto, an extension the plugin is not built with.
func optionFindings(t *testing.T, breaking bool, rules ...config.Rule) map[string]int {
	t.Helper()
	specs, err := house.Specs(rules)
	if err != nil {
		t.Fatal(err)
	}
	spec := &check.Spec{Rules: specs, Categories: []*check.CategorySpec{house.CategorySpec}}
	checktest.SpecTest(t, spec)
	files := func(dir string) *checktest.ProtoFileSpec {
		return &checktest.ProtoFileSpec{DirPaths: []string{dir, "../rules/testdata/deps", "../../proto"}, FilePaths: []string{"api.proto"}}
	}
	reqSpec := &checktest.RequestSpec{Files: files("testdata/options/now")}
	if breaking {
		reqSpec.AgainstFiles = files("testdata/options/against")
	}
	request, err := reqSpec.ToRequest(t.Context())
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
		if strings.Contains(a.Message(), "failed to run") || strings.Contains(a.Message(), "could not be read") {
			t.Errorf("%s: %s", a.RuleID(), a.Message())
		}
	}
	return got
}

func assertFindings(t *testing.T, got, want map[string]int) {
	t.Helper()
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

// A check reads every extension set on an element, by full name: messages
// as maps of their fields, enums by name, unset scalars as zero, unset lists
// and maps as empty, and unset messages left out for has(). A method also
// reads its service's options.
func TestOptions(t *testing.T) {
	const method = "options['acme.auth.v1.method']"
	got := optionFindings(t, false,
		rule("HOUSE_TENANT_OK", "service", "options['acme.auth.v1.service'].tenant == 'TENANT_LOCATION'"),
		rule("HOUSE_TENANT_BAD", "service", "options['acme.auth.v1.service'].tenant == 'TENANT_UNSPECIFIED'"),
		rule("HOUSE_CALLERS_NOT_PUBLIC", "method",
			"!('acme.auth.v1.method' in options) || "+method+".callers.size() == 0 || "+
				"service_options['dedo.v1.service'].audience != 'AUDIENCE_EXTERNAL_PUBLIC'"),
		rule("HOUSE_UNSET_MESSAGE_OK", "method", "!('acme.auth.v1.method' in options) || !has("+method+".sample)"),
		rule("HOUSE_ZERO_VALUES_OK", "method", "!('acme.auth.v1.method' in options) || "+method+".anonymous == ("+method+".callers == [])"),
		rule("HOUSE_MAP_OK", "method", "!('acme.auth.v1.method' in options) || "+method+".limits == {} || "+method+".limits['rps'] == 10"),
		rule("HOUSE_SCALAR_OK", "method", "!('acme.auth.v1.profile' in options) || options['acme.auth.v1.profile'] == 'auth'"),
		rule("HOUSE_HAS_OPTIONS", "method", "size(options) > 0"),
		rule("HOUSE_FILE_OK", "file", "options['acme.auth.v1.file'].permissions.all(p, p.grants.size() == 2)"),
	)
	assertFindings(t, got, map[string]int{
		"HOUSE_TENANT_BAD":         1,
		"HOUSE_CALLERS_NOT_PUBLIC": 1, // GetCart, on a public service
		"HOUSE_HAS_OPTIONS":        1, // ListCarts sets none
	})
}

// A breaking rule reads the earlier element's options as against.options,
// resolved from the earlier version's files.
func TestBreakingOptions(t *testing.T) {
	grants := rule("HOUSE_GRANTS_KEPT", "file", "against.options['acme.auth.v1.file'].permissions == options['acme.auth.v1.file'].permissions")
	callers := rule("HOUSE_CALLERS_KEPT", "method",
		"!('acme.auth.v1.method' in against.options) || against.options['acme.auth.v1.method'].callers == options['acme.auth.v1.method'].callers")
	grants.Breaking, callers.Breaking = true, true
	got := optionFindings(t, true, grants, callers)
	assertFindings(t, got, map[string]int{
		"HOUSE_GRANTS_KEPT":  1, // a grant was added
		"HOUSE_CALLERS_KEPT": 1, // GetCart lost a caller
	})
}
