package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scalarJS stands in for Scalar's bundle. Its text would end an inline
// script, so it must reach the page without being parsed as HTML.
const scalarJS = `window.Scalar = {createApiReference: function () {}}; // </script><!-- <script>`

func sri(b []byte) string {
	sum := sha512.Sum384(b)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// scriptServer serves body over HTTPS, as a CDN does, and counts requests.
func scriptServer(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client := docsClient
	docsClient = srv.Client()
	t.Cleanup(func() { docsClient = client })
	return srv, &hits
}

// stubCache points the script cache at a fresh directory.
func stubCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cache := docsCacheDir
	docsCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { docsCacheDir = cache })
	return dir
}

// staticPage is docs/index.html as dedo's template writes it: a pinned
// remote script, then the call that loads /openapi.json.
func staticPage(src, integrity string) string {
	return `<!doctype html>
<html><head><title>Shop API</title></head>
<body>
<div id="reference"></div>
<script src="` + src + `"
        integrity="` + integrity + `" crossorigin="anonymous"></script>
<script>
  Scalar.createApiReference('#reference', {
    url: '/openapi.json',
    telemetry: false,
  })
</script>
</body></html>
`
}

// The spec here would close the inline script if copied in as it is.
const staticSpec = `{
  "openapi": "3.2.0",
  "info": {"title": "Shop API", "version": "v1"},
  "tags": [{"name": "Orders", "description": "Ends a script: </script><!-- & more"}]
}`

func staticRun(t *testing.T, dir string) (string, string, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "out", "shop.html")
	var out bytes.Buffer
	err := docsStatic(dir, file, &out)
	if err != nil {
		return "", out.String(), err
	}
	b, rerr := os.ReadFile(file)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return string(b), out.String(), nil
}

// The page comes out as one file: the spec inline in place of its URL, and
// the remote script, checked against its integrity hash, as a data: URL.
// A second build needs no network: the script comes from the cache.
func TestDocsStaticInlinesSpecAndScript(t *testing.T) {
	srv, hits := scriptServer(t, scalarJS)
	stubCache(t)
	src := srv.URL + "/npm/@scalar/api-reference@1.75.1/dist/browser/standalone.js"
	dir := repo(t, map[string]string{
		"docs/index.html":   staticPage(src, sri([]byte(scalarJS))),
		"docs/openapi.json": staticSpec,
	})
	got, out, err := staticRun(t, dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	data := `<script src="data:text/javascript;base64,` + base64.StdEncoding.EncodeToString([]byte(scalarJS)) + `"></script>`
	for _, want := range []string{
		data,
		`content: {"openapi":"3.2.0","info":{"title":"Shop API","version":"v1"},"tags":`,
		`Ends a script: \u003c/script\u003e\u003c!-- \u0026 more`,
		"telemetry: false,",
		"<title>Shop API</title>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page lacks %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"/openapi.json", src, "integrity=", "</script><!--"} {
		if strings.Contains(got, bad) {
			t.Errorf("page still has %q:\n%s", bad, got)
		}
	}
	if strings.Count(got, "</script>") != 2 {
		t.Errorf("want two script ends, the bundle's and the call's:\n%s", got)
	}
	if !strings.Contains(out, "no network needed") || *hits != 1 {
		t.Errorf("hits %d, output: %s", *hits, out)
	}

	srv.Close()
	again, out, err := staticRun(t, dir)
	if err != nil || again != got {
		t.Errorf("offline rebuild (%v) differs or failed: %s", err, out)
	}
}

// A cached copy that no longer matches the hash is fetched again.
func TestDocsStaticRefetchesBadCache(t *testing.T) {
	srv, hits := scriptServer(t, scalarJS)
	cache := stubCache(t)
	integrity := sri([]byte(scalarJS))
	dir := repo(t, map[string]string{
		"docs/index.html":   staticPage(srv.URL+"/s.js", integrity),
		"docs/openapi.json": staticSpec,
	})
	if err := os.WriteFile(filepath.Join(cache, cacheName(integrity)), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out, err := staticRun(t, dir); err != nil || *hits != 1 {
		t.Fatalf("hits %d, err %v: %s", *hits, err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(cache, cacheName(integrity))); string(b) != scalarJS {
		t.Errorf("cache holds %q", b)
	}
}

// What dedo cannot check or place, it refuses, and writes nothing.
func TestDocsStaticRefuses(t *testing.T) {
	srv, _ := scriptServer(t, scalarJS)
	good := sri([]byte(scalarJS))
	for _, c := range []struct {
		name, page, spec, want string
	}{
		{"hash mismatch", staticPage(srv.URL+"/s.js", sri([]byte("other"))), staticSpec, "do not match"},
		{"no integrity", strings.Replace(staticPage(srv.URL+"/s.js", good), `integrity="`+good+`" `, "", 1), staticSpec, "no integrity hash"},
		{"plain http", staticPage("http://cdn.example/s.js", good), staticSpec, "not https"},
		{"no spec url", strings.Replace(staticPage(srv.URL+"/s.js", good), "url: '/openapi.json',", "", 1), staticSpec, "dedo templates docs/index.html"},
		{"two spec urls", strings.Replace(staticPage(srv.URL+"/s.js", good), "telemetry: false,", "url: '/openapi.json',", 1), staticSpec, "dedo templates docs/index.html"},
		{"bad spec", staticPage(srv.URL+"/s.js", good), `{"openapi":`, "docs/openapi.json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stubCache(t)
			dir := repo(t, map[string]string{"docs/index.html": c.page, "docs/openapi.json": c.spec})
			file := filepath.Join(t.TempDir(), "shop.html")
			err := docsStatic(dir, file, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
			if _, serr := os.Stat(file); serr == nil {
				t.Error("wrote the page anyway")
			}
		})
	}
}

// Without the generated reference there is nothing to write.
func TestDocsStaticNeedsGeneratedReference(t *testing.T) {
	dir := repo(t, map[string]string{"docs/index.html": "<html></html>"})
	err := docsStatic(dir, filepath.Join(t.TempDir(), "x.html"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "make generate") {
		t.Errorf("err = %v", err)
	}
}

// dedo docs -static writes the file and serves nothing, so it refuses the
// serving flags.
func TestDocsStaticCommand(t *testing.T) {
	srv, _ := scriptServer(t, scalarJS)
	stubCache(t)
	dir := repo(t, map[string]string{
		"docs/index.html":   staticPage(srv.URL+"/s.js", sri([]byte(scalarJS))),
		"docs/openapi.json": staticSpec,
	})
	file := filepath.Join(t.TempDir(), "shop.html")
	var out bytes.Buffer
	if err := run([]string{"docs", "-dir", dir, "-static", file}, &out); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if _, err := os.Stat(file); err != nil {
		t.Error(err)
	}
	for _, extra := range [][]string{{"-tunnel"}, {"-addr", "127.0.0.1:0"}} {
		args := append([]string{"docs", "-dir", dir, "-static", file}, extra...)
		if err := run(args, &out); err == nil || !strings.Contains(err.Error(), "serves nothing") {
			t.Errorf("%v: err = %v", extra, err)
		}
	}
}

// A CDN error is reported with the URL, and nothing is cached.
func TestDocsStaticCDNError(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	client := docsClient
	docsClient = srv.Client()
	t.Cleanup(func() { docsClient = client })
	cache := stubCache(t)
	dir := repo(t, map[string]string{
		"docs/index.html":   staticPage(srv.URL+"/gone.js", sri([]byte(scalarJS))),
		"docs/openapi.json": staticSpec,
	})
	_, _, err := staticRun(t, dir)
	if err == nil || !strings.Contains(err.Error(), "/gone.js") || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("cached %v", entries)
	}
}

// Integrity is checked as browsers check it: any hash of the strongest
// algorithm listed must match, and weaker ones are ignored.
func TestMatchesIntegrity(t *testing.T) {
	b := []byte(scalarJS)
	sha256sum := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	}
	for _, c := range []struct {
		integrity string
		want      bool
	}{
		{sri(b), true},
		{sri(b) + "?opts", true},
		{sri([]byte("x")) + " " + sri(b), true},
		{sha256sum(b), true},
		{sha256sum(b) + " " + sri([]byte("x")), false},
		{sha256sum([]byte("x")) + " " + sri(b), true},
		{"md5-abc", false},
		{"", false},
	} {
		if got := matchesIntegrity(b, c.integrity); got != c.want {
			t.Errorf("matchesIntegrity(%q) = %v, want %v", c.integrity, got, c.want)
		}
	}
}

// An inlined script keeps how it runs (type, defer) and loses only where it
// came from; data-src is not its src.
func TestDocsStaticKeepsScriptAttributes(t *testing.T) {
	srv, _ := scriptServer(t, scalarJS)
	stubCache(t)
	page := strings.Replace(staticPage(srv.URL+"/s.js", sri([]byte(scalarJS))),
		`<script src=`, `<script type="module" data-src="https://elsewhere.example/x.js" defer src=`, 1)
	dir := repo(t, map[string]string{"docs/index.html": page, "docs/openapi.json": staticSpec})
	got, out, err := staticRun(t, dir)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := `<script type="module" data-src="https://elsewhere.example/x.js" defer src="data:text/javascript;base64,`
	if !strings.Contains(got, want) {
		t.Errorf("page lacks %q:\n%s", want, got)
	}
}
