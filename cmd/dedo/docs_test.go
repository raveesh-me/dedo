package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// docsRepo is a service with a reference and a guide beside it.
func docsRepo(t *testing.T) string {
	t.Helper()
	return repo(t, map[string]string{
		"docs/index.html":      "<html>reference</html>",
		"docs/openapi.json":    `{"openapi":"3.1.0"}`,
		"docs/implementing.md": "internal guide",
		"docs/docs.go":         "package docs",
	})
}

// Only the page and the OpenAPI document are served; the guides and code in
// docs/, and anything outside it, are not.
func TestDocsHandlerServesOnlyTheReference(t *testing.T) {
	srv := httptest.NewServer(docsHandler(filepath.Join(docsRepo(t), "docs")))
	defer srv.Close()
	for _, c := range []struct {
		method, path string
		status       int
		body, ctype  string
	}{
		{"GET", "/", 200, "<html>reference</html>", "text/html; charset=utf-8"},
		{"GET", "/index.html", 200, "<html>reference</html>", "text/html; charset=utf-8"},
		{"GET", "/openapi.json", 200, `{"openapi":"3.1.0"}`, "application/json"},
		{"HEAD", "/openapi.json", 200, "", "application/json"},
		{"GET", "/implementing.md", 404, "", ""},
		{"GET", "/docs.go", 404, "", ""},
		{"GET", "/../go.mod", 404, "", ""},
		{"POST", "/openapi.json", 405, "", ""},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, res.StatusCode, c.status)
		}
		if c.status == 200 && (string(body) != c.body || res.Header.Get("Content-Type") != c.ctype) {
			t.Errorf("%s %s: %q (%s)", c.method, c.path, body, res.Header.Get("Content-Type"))
		}
	}
}

// A regenerated document shows without a restart.
func TestDocsHandlerReadsFreshFiles(t *testing.T) {
	dir := docsRepo(t)
	srv := httptest.NewServer(docsHandler(filepath.Join(dir, "docs")))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "docs/openapi.json"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if body, _ := io.ReadAll(res.Body); string(body) != `{"v":2}` {
		t.Errorf("served %q", body)
	}
}

func TestDocsNeedsGeneratedReference(t *testing.T) {
	dir := repo(t, map[string]string{"docs/index.html": "<html></html>"})
	err := docs(context.Background(), dir, "127.0.0.1:0", false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "docs/openapi.json") || !strings.Contains(err.Error(), "make generate") {
		t.Errorf("err = %v", err)
	}
}

func TestDocsTunnelNeedsCloudflared(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	err := docs(context.Background(), docsRepo(t), "127.0.0.1:0", true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cloudflared") {
		t.Errorf("err = %v", err)
	}
}

// Without -tunnel the reference is served on loopback until ctx ends.
func TestDocsServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- docs(ctx, docsRepo(t), "127.0.0.1:0", false, out) }()
	local := waitFor(t, out, regexp.MustCompile(`http://127\.0\.0\.1:\d+`))
	res, err := http.Get(local + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("status %d", res.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("docs returned %v after Ctrl-C", err)
	}
	if _, err := http.Get(local + "/"); err == nil {
		t.Error("still serving after Ctrl-C")
	}
}

// With -tunnel, cloudflared is pointed at the local server, only its public
// URL and real errors are shown, and Ctrl-C stops it without an error.
func TestDocsTunnel(t *testing.T) {
	bin := t.TempDir()
	args := filepath.Join(bin, "args")
	fake := filepath.Join(bin, "cloudflared")
	script := "#!/bin/sh\necho \"$@\" > " + args + "\n" +
		"echo '2026-11-03T04:45:12Z INF Requesting new quick Tunnel on trycloudflare.com...' >&2\n" +
		"echo '2026-11-03T04:45:13Z INF |  https://quiet-bird-sings.trycloudflare.com  |' >&2\n" +
		"echo '2026-11-03T04:45:14Z ERR failed to sufficiently increase receive buffer size' >&2\n" +
		"trap 'exit 0' INT\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	stubLookPath(t, func(string) (string, error) { return fake, nil })

	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- docs(ctx, docsRepo(t), "127.0.0.1:0", true, out) }()
	waitFor(t, out, regexp.MustCompile(`increase receive buffer`))
	cancel()
	if err := <-done; err != nil {
		t.Errorf("docs returned %v after Ctrl-C", err)
	}
	s := out.String()
	if !strings.Contains(s, "shared at https://quiet-bird-sings.trycloudflare.com/") || strings.Contains(s, "Requesting new quick Tunnel") {
		t.Errorf("output:\n%s", s)
	}
	local := regexp.MustCompile(`http://127\.0\.0\.1:\d+`).FindString(s)
	if got, _ := os.ReadFile(args); strings.TrimSpace(string(got)) != "tunnel --no-autoupdate --url "+local {
		t.Errorf("cloudflared args %q, want the local server %s", got, local)
	}
}

func stubLookPath(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	saved := lookPath
	lookPath = f
	t.Cleanup(func() { lookPath = saved })
}

// waitFor waits until out matches re and returns the match.
func waitFor(t *testing.T, out *syncBuffer, re *regexp.Regexp) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if m := re.FindString(out.String()); m != "" {
			return m
		}
	}
	t.Fatalf("no %s in output:\n%s", re, out.String())
	return ""
}

// syncBuffer is a bytes.Buffer safe to write and read from two goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
