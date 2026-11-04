package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxScript caps a script dedo docs -static downloads to inline.
const maxScript = 64 << 20

// docsClient downloads the scripts the page loads; tests replace it.
var docsClient = &http.Client{Timeout: 2 * time.Minute}

// docsCacheDir is where downloaded scripts are kept, by integrity hash, so a
// later build needs no network; tests replace it.
var docsCacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	return filepath.Join(dir, "dedo", "scripts"), err
}

var (
	// specURL is the option in docs/index.html that loads the OpenAPI
	// document; -static puts the document itself in its place.
	specURL = regexp.MustCompile(`url:\s*['"]/openapi\.json['"]`)
	// emptyScript is a script element with no inline code: one that loads src.
	emptyScript = regexp.MustCompile(`(?is)<script\b([^>]*)>\s*</script\s*>`)
	srcAttr     = regexp.MustCompile(`(?is)(?:^|\s)src\s*=\s*["']([^"']*)["']`)
	sriAttr     = regexp.MustCompile(`(?is)(?:^|\s)integrity\s*=\s*["']([^"']*)["']`)
	// fetchAttrs are the attributes that say where and how to fetch a
	// script; an inlined one drops them and keeps the rest (type, defer).
	fetchAttrs = regexp.MustCompile(`(?is)\s+(?:src|integrity|crossorigin)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?`)
)

// docsStatic writes the REST reference of the service in dir as one HTML
// file that opens from disk with no network: docs/openapi.json inline in
// place of its URL, and each script the page loads, checked against its
// integrity hash, inline as a data: URL.
func docsStatic(dir, file string, out io.Writer) error {
	root := filepath.Join(dir, "docs")
	page, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		return fmt.Errorf("docs/index.html not found in %s: run make generate first (%w)", dir, err)
	}
	spec, err := os.ReadFile(filepath.Join(root, "openapi.json"))
	if err != nil {
		return fmt.Errorf("docs/openapi.json not found in %s: run make generate first (%w)", dir, err)
	}
	html, err := inlineSpec(string(page), spec)
	if err != nil {
		return err
	}
	if html, err = inlineScripts(html, out); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	if err := os.WriteFile(file, []byte(html), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	fmt.Fprintf(out, "dedo docs: wrote %s (%.1f MB), the REST reference in one file: open it from disk, no network needed\n",
		file, float64(len(html))/(1<<20))
	return nil
}

// inlineSpec replaces the page's one url: '/openapi.json' with the document.
// The JSON is compacted, keeping its key order, and <, > and & are escaped
// so no text in it can end the script it sits in.
func inlineSpec(page string, spec []byte) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, spec); err != nil {
		return "", fmt.Errorf("docs/openapi.json is not valid JSON: run make generate (%w)", err)
	}
	var safe bytes.Buffer
	json.HTMLEscape(&safe, compact.Bytes())
	at := specURL.FindAllStringIndex(page, -1)
	if len(at) != 1 {
		return "", fmt.Errorf("docs/index.html loads /openapi.json %d times, not once as dedo's page does: refresh it with dedo templates docs/index.html", len(at))
	}
	return page[:at[0][0]] + "content: " + safe.String() + page[at[0][1]:], nil
}

// inlineScripts replaces each script the page loads from elsewhere with its
// verified bytes as a data: URL, which the HTML parser never reads as markup.
func inlineScripts(page string, out io.Writer) (string, error) {
	var b strings.Builder
	last := 0
	for _, m := range emptyScript.FindAllStringSubmatchIndex(page, -1) {
		attrs := page[m[2]:m[3]]
		src := srcAttr.FindStringSubmatch(attrs)
		if src == nil {
			continue
		}
		body, err := verifiedScript(src[1], sriAttr.FindStringSubmatch(attrs), out)
		if err != nil {
			return "", err
		}
		b.WriteString(page[last:m[0]])
		b.WriteString("<script")
		if kept := strings.TrimSpace(fetchAttrs.ReplaceAllString(" "+attrs, "")); kept != "" {
			b.WriteString(" " + kept)
		}
		b.WriteString(` src="data:text/javascript;base64,`)
		b.WriteString(base64.StdEncoding.EncodeToString(body))
		b.WriteString(`"></script>`)
		last = m[1]
	}
	b.WriteString(page[last:])
	return b.String(), nil
}

// verifiedScript returns the script at src, from the cache or downloaded,
// once its bytes match the page's integrity hash.
func verifiedScript(src string, sri []string, out io.Writer) ([]byte, error) {
	if !strings.HasPrefix(src, "https://") {
		return nil, fmt.Errorf("docs/index.html loads %s, which is not https: dedo inlines only scripts it can check", src)
	}
	if sri == nil || strings.TrimSpace(sri[1]) == "" {
		return nil, fmt.Errorf("docs/index.html loads %s with no integrity hash, so dedo cannot check what it would inline: add one, or refresh the page with dedo templates docs/index.html", src)
	}
	integrity := sri[1]
	cache, cerr := docsCacheDir()
	cached := filepath.Join(cache, cacheName(integrity))
	if cerr == nil {
		if b, err := os.ReadFile(cached); err == nil && matchesIntegrity(b, integrity) {
			return b, nil
		}
	}
	b, err := download(src)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w (dedo needs the network once per script version; after that, builds use the cached copy)", src, err)
	}
	if !matchesIntegrity(b, integrity) {
		return nil, fmt.Errorf("fetched %s, but its bytes do not match the page's integrity hash %s", src, integrity)
	}
	if err := writeCache(cache, cerr, cached, b); err != nil {
		fmt.Fprintf(out, "dedo docs: could not cache %s (%v); the next build fetches it again\n", src, err)
	}
	return b, nil
}

func download(src string) ([]byte, error) {
	res, err := docsClient.Get(src)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxScript+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxScript {
		return nil, fmt.Errorf("larger than %d MB", maxScript>>20)
	}
	return b, nil
}

// writeCache keeps b at path, through a rename so a reader never sees half.
func writeCache(dir string, dirErr error, path string, b []byte) error {
	if dirErr != nil {
		return dirErr
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".script-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// cacheName is the cache file of the script with this integrity hash.
func cacheName(integrity string) string {
	sum := sha256.Sum256([]byte(integrity))
	return hex.EncodeToString(sum[:]) + ".js"
}

// sriHashes are the algorithms of Subresource Integrity, weakest first.
var sriHashes = []struct {
	name string
	new  func() hash.Hash
}{
	{"sha256", sha256.New},
	{"sha384", sha512.New384},
	{"sha512", sha512.New},
}

// matchesIntegrity reports whether b matches an integrity attribute as a
// browser checks it: against the hashes of its strongest algorithm only.
func matchesIntegrity(b []byte, integrity string) bool {
	strongest, matched := -1, false
	for token := range strings.FieldsSeq(integrity) {
		token, _, _ = strings.Cut(token, "?")
		alg, want, ok := strings.Cut(token, "-")
		if !ok {
			continue
		}
		for i, h := range sriHashes {
			if h.name != alg || i < strongest {
				continue
			}
			if i > strongest {
				strongest, matched = i, false
			}
			sum := h.new()
			sum.Write(b)
			if base64.StdEncoding.EncodeToString(sum.Sum(nil)) == want {
				matched = true
			}
		}
	}
	return matched
}
