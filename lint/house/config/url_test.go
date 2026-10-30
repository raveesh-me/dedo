package config

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func sum(s string) string {
	b := sha256.Sum256([]byte(s))
	return hex.EncodeToString(b[:])
}

// serve publishes files over HTTPS for the test, and returns the server's URL.
func serve(t *testing.T, files map[string]string) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := httpClient
	httpClient = srv.Client()
	t.Cleanup(func() { httpClient = prev })
	return srv.URL
}

// importURL writes a service's rules.yml that imports url, pinned to pin.
func importURL(t *testing.T, url, pin string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: "imports:\n  - url: " + url + "\n    sha256: " + pin + "\n"})
	return filepath.Join(dir, RulesFileName)
}

const docRules = `rules:
  - id: HOUSE_RELATIVE_DOC
    on: resource
    check: "true"
    message: m
    doc: RULES.html#house_relative_doc
  - id: HOUSE_ABSOLUTE_DOC
    on: resource
    check: "true"
    message: m
    doc: https://wiki.example.com/rules
`

func TestFetchURLImport(t *testing.T) {
	base := serve(t, map[string]string{"/rules/v1/rules.yml": docRules})
	url := base + "/rules/v1/rules.yml"
	path := importURL(t, url, sum(docRules))

	if _, err := loadRules(path); err == nil || !strings.Contains(err.Error(), "run dedo tools") {
		t.Fatalf("before fetching: %v", err)
	}
	fetched, err := Fetch(path)
	if err != nil || !slices.Equal(fetched, []string{url}) {
		t.Fatalf("Fetch = %v, %v", fetched, err)
	}
	rules, err := loadRules(path)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	for _, r := range rules {
		docs[r.ID] = r.Doc
	}
	if got, want := docs["HOUSE_RELATIVE_DOC"], base+"/rules/v1/RULES.html#house_relative_doc"; got != want {
		t.Errorf("relative doc = %q, want %q", got, want)
	}
	if got, want := docs["HOUSE_ABSOLUTE_DOC"], "https://wiki.example.com/rules"; got != want {
		t.Errorf("absolute doc = %q, want %q", got, want)
	}
	if again, err := Fetch(path); err != nil || len(again) != 0 {
		t.Errorf("second Fetch = %v, %v; want nothing new", again, err)
	}
}

func TestURLImportPin(t *testing.T) {
	base := serve(t, map[string]string{"/v1/rules.yml": domainRule})
	url := base + "/v1/rules.yml"
	for name, tc := range map[string]struct{ pin, want string }{
		"missing": {"", "pin it with sha256: " + sum(domainRule)},
		"wrong":   {sum("other"), "its sha256 is " + sum(domainRule)},
		"not hex": {"abc", "want 64 hex digits"},
	} {
		t.Run(name, func(t *testing.T) {
			path := importURL(t, url, tc.pin)
			_, err := Fetch(path)
			if tc.pin == "abc" {
				_, err = loadRules(path)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := loadRules(importURL(t, url, "")); err == nil || !strings.Contains(err.Error(), "needs a sha256") {
		t.Errorf("Load without a pin: %v", err)
	}
}

func TestURLImportImportsBesideIt(t *testing.T) {
	top := "imports:\n  - path: shared/domain.yml\n    sha256: " + sum(domainRule) + "\n"
	base := serve(t, map[string]string{"/v1/rules.yml": top, "/v1/shared/domain.yml": domainRule})
	path := importURL(t, base+"/v1/rules.yml", sum(top))
	fetched, err := Fetch(path)
	if err != nil || len(fetched) != 2 || fetched[1] != base+"/v1/shared/domain.yml" {
		t.Fatalf("Fetch = %v, %v", fetched, err)
	}
	rules, err := loadRules(path)
	if err != nil || !slices.Equal(ids(rules), []string{"HOUSE_RESOURCE_TYPE_DOMAIN"}) {
		t.Fatalf("rules = %v, %v", ids(rules), err)
	}
}

func TestURLImportRejects(t *testing.T) {
	unpinned := "imports:\n  - path: domain.yml\n"
	escapes := "imports:\n  - path: ../other/domain.yml\n    sha256: " + sum(domainRule) + "\n"
	base := serve(t, map[string]string{"/v1/unpinned.yml": unpinned, "/v1/escapes.yml": escapes})
	for name, tc := range map[string]struct{ url, pin, want string }{
		"plain http":      {"http://example.com/v1/rules.yml", sum("x"), "fetched over https"},
		"a folder":        {base + "/v1/", sum("x"), "names a file"},
		"unpinned import": {base + "/v1/unpinned.yml", sum(unpinned), "domain.yml needs a sha256"},
		"leaves its root": {base + "/v1/escapes.yml", sum(escapes), "outside the repository"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Fetch(importURL(t, tc.url, tc.pin)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A relative doc in the service's own rules, or a git import's, is kept as
// written: only a url import has a URL to resolve it against.
func TestLocalRelativeDocKept(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{RulesFileName: docRules})
	rules, err := loadRules(filepath.Join(dir, RulesFileName))
	if err != nil || rules[0].Doc != "RULES.html#house_relative_doc" {
		t.Fatalf("doc = %q, %v", rules[0].Doc, err)
	}
}
