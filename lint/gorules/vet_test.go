package gorules

import (
	"slices"
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/lint/house/config"
)

func TestVetArgs(t *testing.T) {
	p, err := Consolidate(config.GoSelection{
		Disabled: []string{IDEventDeclared},
		Rules:    []config.GoRule{{ID: "HOUSE_GO_A", ForbidImport: "example.com/sdk/...", Message: "m"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := VetArgs("/bin/dedo", p, "/repo", true, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"vet", "-vettool=/bin/dedo", "-json",
		"-dedo_go_event_declared=false",
		"-dedo_go_server_interceptors.bff",
		`-house_go.config={"root":"/repo","rules":[{"id":"HOUSE_GO_A","forbid_import":"example.com/sdk/...","message":"m"}]}`,
		"./...",
	}
	if !slices.Equal(args, want) {
		t.Errorf("args =\n%q\nwant\n%q", args, want)
	}
	plain, err := Consolidate(config.GoSelection{})
	if err != nil {
		t.Fatal(err)
	}
	args, err = VetArgs("dedo", plain, "/repo", false, []string{"./..."})
	if err != nil || !slices.Contains(args, "-house_go=false") || slices.ContainsFunc(args, func(a string) bool { return strings.HasSuffix(a, "=false") && a != "-house_go=false" }) {
		t.Errorf("without house rules: %q, %v", args, err)
	}
}

const vetOutput = `# example.com/svc/app
{
	"example.com/svc/app": {
		"dedo_go_server_interceptors": [
			{"posn": "/repo/app/app.go:12:2", "end": "/repo/app/app.go:12:30", "category": "DEDO_GO_SERVER_INTERCEPTORS", "message": "DEDO_GO_SERVER_INTERCEPTORS: second"},
			{"posn": "/repo/app/app.go:9:2", "end": "/repo/app/app.go:9:30", "category": "DEDO_GO_SERVER_INTERCEPTORS", "message": "DEDO_GO_SERVER_INTERCEPTORS: first"}
		]
	}
}
# example.com/svc/app [example.com/svc/app.test]
{
	"example.com/svc/app [example.com/svc/app.test]": {
		"dedo_go_server_interceptors": [
			{"posn": "/repo/app/app.go:9:2", "end": "/repo/app/app.go:9:30", "category": "DEDO_GO_SERVER_INTERCEPTORS", "message": "DEDO_GO_SERVER_INTERCEPTORS: first"}
		],
		"house_go": {"error": "house_go -config: unexpected end of JSON input"}
	}
}
{}
`

func TestParseVet(t *testing.T) {
	findings, errs, err := ParseVet(strings.NewReader(vetOutput))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.String())
	}
	want := []string{
		"/repo/app/app.go:9:2: DEDO_GO_SERVER_INTERCEPTORS: first",
		"/repo/app/app.go:12:2: DEDO_GO_SERVER_INTERCEPTORS: second",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "house_go: house_go -config") {
		t.Errorf("errs = %q", errs)
	}
	if _, _, err := ParseVet(strings.NewReader("{")); err == nil {
		t.Error("malformed output is accepted")
	}
	if _, _, err := ParseVet(strings.NewReader(`{"p": {"a": {}}}`)); err == nil {
		t.Error("an analyzer entry with neither findings nor an error is accepted")
	}
}

func TestDocsURL(t *testing.T) {
	prev := DocsRef
	t.Cleanup(func() { DocsRef = prev })
	DocsRef = "v1.2.3"
	if got := DocsURL(IDEventDeclared); got != "https://github.com/raveesh-me/dedo/blob/v1.2.3/guidance/RULES.md#dedo_go_event_declared" {
		t.Errorf("DocsURL = %s", got)
	}
}

func TestGoCamelCase(t *testing.T) {
	for in, want := range map[string]string{
		"GetOrder":        "GetOrder",
		"get_order":       "GetOrder",
		"Order.Line":      "Order_Line",
		"_private":        "XPrivate",
		"v2_thing":        "V2Thing",
		"Order.line_item": "OrderLineItem",
	} {
		if got := goCamelCase(in); got != want {
			t.Errorf("goCamelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseDirective(t *testing.T) {
	for text, want := range map[string][3]string{
		"//dedo:ignore X because":         {"X", "because", "true"},
		"//dedo:ignore X see https://a.b": {"X", "see https://a.b", "true"},
		"//dedo:ignore X // want y":       {"X", "", "true"},
		"//dedo:ignore":                   {"", "", "true"},
		"//dedo:ignored X":                {"", "", "false"},
		"// dedo:ignore X y":              {"", "", "false"},
	} {
		rule, reason, ok := parseDirective(text)
		if got := [3]string{rule, reason, map[bool]string{true: "true", false: "false"}[ok]}; got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
