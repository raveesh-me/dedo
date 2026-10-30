package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// A url import names one rules file served over HTTPS, pinned by its
// SHA-256, such as a release of an organisation's rules published to a
// static site:
//
//	imports:
//	  - url: https://acme.github.io/api-rules/v3/rules.yml
//	    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
//
// The folder of that URL is the import's root, as a repository's is for a
// git import: the file's own path imports are fetched from beside it, stay
// inside that folder, and carry a sha256 each, so every file read over the
// network is pinned. A relative doc on one of its rules is resolved against
// the file's URL, so a finding links to the documentation of the release
// that reported it.

// maxRulesFile bounds a downloaded rules file.
const maxRulesFile = 1 << 20

// urlMarker, in the cache folder of a url import, holds the URL of the
// import's root folder.
const urlMarker = ".url"

// httpClient fetches url imports; tests replace it.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// rootURL returns the folder of a url import, with a trailing slash.
func rootURL(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("the import of %s: %w", raw, err)
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("the import of %s: a url import is fetched over https", raw)
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" || strings.HasSuffix(u.Path, "/") {
		return "", "", fmt.Errorf("the import of %s: a url import names a file, such as .../v3/rules.yml", raw)
	}
	u.Path = path.Dir(u.Path) + "/"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), name, nil
}

// urlOf returns the root URL of a url import's cache folder, or "" when
// repo is not one.
func urlOf(repo string) string {
	if repo == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(repo, urlMarker))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// fileURL is the URL a file of a url import's cache folder was fetched from.
func fileURL(repo, file string) (*url.URL, error) {
	rel, err := filepath.Rel(repo, file)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(urlOf(repo))
	if err != nil {
		return nil, err
	}
	return base.Parse(filepath.ToSlash(rel))
}

// resolveDoc turns a rule's relative doc into a URL beside the url-imported
// file that defines it. Other docs are kept as written.
func resolveDoc(doc, repo, file string) string {
	if doc == "" || urlOf(repo) == "" {
		return doc
	}
	ref, err := url.Parse(doc)
	if err != nil || ref.IsAbs() {
		return doc
	}
	from, err := fileURL(repo, file)
	if err != nil {
		return doc
	}
	return from.ResolveReference(ref).String()
}

// overHTTPS reports whether an import is read over the network, and so
// must be pinned: a url import, or a path import of a url-imported file.
func overHTTPS(imp Import, repo string) bool {
	return imp.URL != "" || imp.Git == "" && urlOf(repo) != ""
}

// checkPin reports an import fetched over the network without a valid sha256.
func checkPin(from, name, sum string) error {
	if sum == "" {
		return fmt.Errorf("%s: the import of %s needs a sha256, so every machine reads the same file; dedo tools prints it", from, name)
	}
	if b, err := hex.DecodeString(sum); err != nil || len(b) != sha256.Size {
		return fmt.Errorf("%s: the import of %s has sha256 %q, want 64 hex digits", from, name, sum)
	}
	return nil
}

// fetchURL downloads a url import into dir, through a temporary folder, so
// an interrupted fetch leaves nothing half-done.
func fetchURL(imp Import, dir string) error {
	root, name, err := rootURL(imp.URL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".fetch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := download(imp.URL, imp.SHA256, filepath.Join(tmp, name)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, urlMarker), []byte(root+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// fetchBeside downloads a path import of a url-imported file, from beside it,
// unless it is fetched already.
func fetchBeside(repo, target, sum string) (bool, error) {
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	from, err := fileURL(repo, target)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	tmp := target + ".fetch"
	defer os.Remove(tmp)
	if err := download(from.String(), sum, tmp); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, target)
}

// download writes the file at raw to dst, if its SHA-256 is want. Without
// want it fails, naming the file's SHA-256 to pin.
func download(raw, want, dst string) error {
	resp, err := httpClient.Get(raw)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: %s", raw, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRulesFile+1))
	if err != nil {
		return fmt.Errorf("fetching %s: %w", raw, err)
	}
	if len(b) > maxRulesFile {
		return fmt.Errorf("fetching %s: larger than %d bytes", raw, maxRulesFile)
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	switch {
	case want == "":
		return fmt.Errorf("fetching %s: no sha256 to check it against; if you trust this file, pin it with sha256: %s", raw, got)
	case !strings.EqualFold(want, got):
		return fmt.Errorf("fetching %s: its sha256 is %s, not the %s pinned: the file changed since it was pinned; check the change, then pin the new sum", raw, got, want)
	}
	return os.WriteFile(dst, b, 0o644)
}
