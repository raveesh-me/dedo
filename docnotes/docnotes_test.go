package docnotes_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/docnotes"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRead(t *testing.T) {
	a := write(t, "a.json", `{"methods": {
		"acme.v1.Svc.Get": {"description": " Only for some. ", "extensions": {"x-plans": {"plans": ["pro"]}}},
		"acme.v1.Svc.List": {"extensions": {"x-plans": true}}
	}}`)
	b := write(t, "b.json", `{"methods": {"acme.v1.Svc.Get": {"description": "And audited.", "extensions": {"x-audit": "yes"}}}}`)
	notes, err := docnotes.Read([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	get := notes["acme.v1.Svc.Get"]
	if get.Description != "Only for some.\n\nAnd audited." {
		t.Errorf("description = %q: want the files' paragraphs in order", get.Description)
	}
	if string(get.Extensions["x-plans"]) != `{"plans": ["pro"]}` || string(get.Extensions["x-audit"]) != `"yes"` {
		t.Errorf("extensions = %s", get.Extensions)
	}
	if list := notes["acme.v1.Svc.List"]; list.Description != "" || string(list.Extensions["x-plans"]) != "true" {
		t.Errorf("List = %+v", list)
	}
	if notes, err := docnotes.Read(nil); err != nil || len(notes) != 0 {
		t.Errorf("no files: %v, %v", notes, err)
	}
}

func TestReadRejects(t *testing.T) {
	ok := write(t, "ok.json", `{"methods": {"acme.v1.Svc.Get": {"extensions": {"x-plans": 1}}}}`)
	tests := []struct {
		name    string
		files   []string
		wantErr string
	}{
		{"a missing file", []string{filepath.Join(t.TempDir(), "none.json")}, "none.json"},
		{"not JSON", []string{write(t, "bad.json", `{"methods":`)}, "bad.json"},
		{"an unknown field", []string{write(t, "typo.json", `{"methods": {"acme.v1.Svc.Get": {"descripton": "x"}}}`)}, "descripton"},
		{"an unknown top-level field", []string{write(t, "top.json", `{"method": {}}`)}, `"method"`},
		{"an extension without x-", []string{write(t, "nox.json", `{"methods": {"acme.v1.Svc.Get": {"extensions": {"plans": 1}}}}`)}, `"plans" does not start with x-`},
		{"no method name", []string{write(t, "noname.json", `{"methods": {" ": {"description": "x"}}}`)}, "names no method"},
		{"one extension twice", []string{ok, write(t, "again.json", `{"methods": {"acme.v1.Svc.Get": {"extensions": {"x-plans": 2}}}}`)}, "x-plans twice"},
		{"trailing data", []string{write(t, "two.json", `{"methods": {}} {}`)}, "after the notes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := docnotes.Read(tt.files)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
