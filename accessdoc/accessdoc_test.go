package accessdoc_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/accessdoc"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// founders is what commerce-auth's protoc-gen-commerce-iam writes for a
// scope its permissions condition.
const founders = `{"scopes": {"taxes/tariff-codes.readonly": {
	"description": "Only for: Agencies on the Founders plan (` + "`founders`" + `). Other callers get 403 ` + "`PERMISSION_DENIED`" + ` with reason ` + "`CONDITION_NOT_MET`" + `.",
	"extensions": {
		"x-commerce-conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}],
		"x-commerce-permissions": [{"id": "taxes/classification.view", "label": "Use tariff code suggestions"}]
	},
	"meta": {"conditions": [{"id": "founders", "label": "Agencies on the Founders plan"}]}
}}}`

func TestRead(t *testing.T) {
	a := write(t, "a.json", founders)
	b := write(t, "b.json", `{"scopes": {
		"taxes/tariff-codes.readonly": {"description": " And audited. ", "extensions": {"x-audit": true}, "meta": {"audited": true}},
		"taxes/tax-codes.readonly": {}
	}}`)
	reqs, err := accessdoc.Read([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	r := reqs["taxes/tariff-codes.readonly"]
	if !strings.HasPrefix(r.Description, "Only for: Agencies on the Founders plan") || !strings.HasSuffix(r.Description, "`CONDITION_NOT_MET`.\n\nAnd audited.") {
		t.Errorf("description = %q: want the files' paragraphs in parameter order", r.Description)
	}
	if len(r.Extensions) != 3 || r.Extensions["x-audit"] == nil || len(r.Meta) != 2 || r.Meta["conditions"] == nil {
		t.Errorf("extensions %s, meta %s", r.Extensions, r.Meta)
	}
	if reqs, err := accessdoc.Read(nil); err != nil || len(reqs) != 0 {
		t.Errorf("no files: %v, %v", reqs, err)
	}
}

// For gives an RPC the entry of each of its scopes, in scope order, and
// nothing for a scope no file names.
func TestFor(t *testing.T) {
	reqs, err := accessdoc.Read([]string{write(t, "a.json", `{"scopes": {
		"vault.open":  {"description": "Only for open vaults.", "extensions": {"x-open": 1}, "meta": {"open": 1}},
		"vault.admin": {"description": "Only for admins.", "extensions": {"x-admin": 2}, "meta": {"admin": 2}},
		"vault.other": {"description": "Unused."}
	}}`)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reqs.For([]string{"vault.admin", "vault.unknown", "vault.open"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Only for admins.\n\nOnly for open vaults." {
		t.Errorf("description = %q, want the scopes' paragraphs in scope order", got.Description)
	}
	if string(got.Extensions["x-admin"]) != "2" || string(got.Extensions["x-open"]) != "1" || len(got.Extensions) != 2 {
		t.Errorf("extensions = %s", got.Extensions)
	}
	if string(got.Meta["admin"]) != "2" || string(got.Meta["open"]) != "1" || len(got.Meta) != 2 {
		t.Errorf("meta = %s", got.Meta)
	}
	if none, err := reqs.For([]string{"vault.unknown"}); err != nil || none.Description != "" || none.Extensions != nil || none.Meta != nil {
		t.Errorf("unknown scope: %+v, %v", none, err)
	}
	if none, err := accessdoc.Requirements(nil).For([]string{"vault.open"}); err != nil || none.Description != "" {
		t.Errorf("no requirements: %+v, %v", none, err)
	}
	b, err := got.MetaJSON()
	if err != nil || string(b) != `{"admin":2,"open":1}` {
		t.Errorf("MetaJSON = %s, %v", b, err)
	}
}

// One extension or meta key twice for one RPC is an error, whether two
// scopes or two files give it.
func TestForRejectsTwice(t *testing.T) {
	reqs, err := accessdoc.Read([]string{write(t, "a.json", `{"scopes": {
		"a": {"extensions": {"x-plans": 1}, "meta": {"plans": 1}},
		"b": {"extensions": {"x-plans": 2}},
		"c": {"meta": {"plans": 2}}
	}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reqs.For([]string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "x-plans") {
		t.Errorf("err = %v, want extension x-plans twice", err)
	}
	if _, err := reqs.For([]string{"a", "c"}); err == nil || !strings.Contains(err.Error(), `meta "plans"`) {
		t.Errorf("err = %v, want meta plans twice", err)
	}
}

func TestReadRejects(t *testing.T) {
	ok := write(t, "ok.json", `{"scopes": {"s": {"extensions": {"x-plans": 1}, "meta": {"plans": 1}}}}`)
	tests := []struct {
		name    string
		files   []string
		wantErr string
	}{
		{"a missing file", []string{filepath.Join(t.TempDir(), "none.json")}, "none.json"},
		{"not JSON", []string{write(t, "bad.json", `{"scopes":`)}, "bad.json"},
		{"an unknown field", []string{write(t, "typo.json", `{"scopes": {"s": {"descripton": "x"}}}`)}, "descripton"},
		{"the per-method format", []string{write(t, "methods.json", `{"methods": {}}`)}, `"methods"`},
		{"an extension without x-", []string{write(t, "nox.json", `{"scopes": {"s": {"extensions": {"plans": 1}}}}`)}, `"plans" does not start with x-`},
		{"no scope", []string{write(t, "noname.json", `{"scopes": {" ": {"description": "x"}}}`)}, "names no scope"},
		{"an empty meta key", []string{write(t, "nometa.json", `{"scopes": {"s": {"meta": {"": 1}}}}`)}, "empty meta key"},
		{"one extension twice", []string{ok, write(t, "again.json", `{"scopes": {"s": {"extensions": {"x-plans": 2}}}}`)}, `extension "x-plans" twice`},
		{"one meta key twice", []string{ok, write(t, "meta.json", `{"scopes": {"s": {"meta": {"plans": 2}}}}`)}, `meta "plans" twice`},
		{"trailing data", []string{write(t, "two.json", `{"scopes": {}} {}`)}, "after the requirements"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := accessdoc.Read(tt.files)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// MetaJSON is the RPC's meta as one compact JSON object, keys sorted: the
// same requirements always give the same bytes.
func TestMetaJSON(t *testing.T) {
	r := accessdoc.Requirement{Meta: map[string]json.RawMessage{"b": json.RawMessage(`[ 1, 2 ]`), "a": json.RawMessage(`{ "x" : "<y>" }`)}}
	b, err := r.MetaJSON()
	if err != nil || string(b) != `{"a":{"x":"\u003cy\u003e"},"b":[1,2]}` {
		t.Errorf("MetaJSON = %s, %v", b, err)
	}
	if b, err := (accessdoc.Requirement{}).MetaJSON(); err != nil || b != nil {
		t.Errorf("empty MetaJSON = %s, %v", b, err)
	}
}
