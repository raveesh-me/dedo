package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// An Annexure is guidance for coding agents that dedo adds to a repository's
// CLAUDE.md and AGENTS.md, after its own: an organisation's conventions, such
// as the headers its platform requires. dedo's guidance tells agents that an
// annexure wins where the two conflict. dedo.yaml lists them:
//
//	annexure:
//	  - url: https://acme.github.io/api-rules/v3/AGENTS.md
//	    sha256: 9f86d0…   # the file's SHA-256, as for a url import
//	  - path: docs/agents.md  # relative to dedo.yaml's folder
//
// dedo init adds them, and dedo upgrade rewrites them from their sources, so
// the copies in CLAUDE.md and AGENTS.md stay what dedo.yaml names.
type Annexure struct {
	Path   string `yaml:"path"`
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
}

// Name is the path or URL of the annexure, as dedo.yaml names it.
func (a Annexure) Name() string {
	if a.URL != "" {
		return a.URL
	}
	return a.Path
}

// Annexures returns the annexures a dedo.yaml lists, checked: each names a
// path or an https URL, and a sha256 only a url, as 64 hex digits. A rules
// file has none.
func Annexures(path string) ([]Annexure, error) {
	if !IsProject(path) {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := decode(path, b, true)
	if err != nil {
		return nil, err
	}
	for _, a := range p.Annexure {
		if err := checkAnnexure(path, a); err != nil {
			return nil, err
		}
	}
	return p.Annexure, nil
}

func checkAnnexure(from string, a Annexure) error {
	switch {
	case (a.Path == "") == (a.URL == ""):
		return fmt.Errorf("%s: an annexure names a path or a url, one of them", from)
	case a.Path != "":
		if a.SHA256 != "" {
			return fmt.Errorf("%s: the annexure %s is in the repository; only a url annexure has a sha256", from, a.Path)
		}
		return nil
	}
	u, err := url.Parse(a.URL)
	if err != nil {
		return fmt.Errorf("%s: the annexure %s: %w", from, a.URL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s: the annexure %s: a url annexure is fetched over https", from, a.URL)
	}
	if a.SHA256 == "" {
		return nil // ReadAnnexure fails, naming the sum to pin
	}
	return checkPin(from, a.URL, a.SHA256)
}

// ReadAnnexure returns the Markdown of an annexure of the dedo.yaml in root:
// a url annexure fetched and checked against its sha256 (when it has none,
// the error names the sum to pin), a path one read from the repository.
func ReadAnnexure(root string, a Annexure) ([]byte, error) {
	if a.URL != "" {
		return get(a.URL, a.SHA256)
	}
	b, err := os.ReadFile(filepath.Join(root, a.Path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the annexure %s is not in %s", a.Path, root)
	}
	return b, err
}
