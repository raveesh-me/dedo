// Package config reads a repository's house rules: rules.yml next to
// buf.yaml, and the rules files it imports from folders and git
// repositories. It has no CEL in it, so the dedo command can fetch imports
// with it too.
//
//	imports:
//	  - path: platform-rules            # a folder (its rules.yml) or a file
//	  - git: https://github.com/acme/api-standards.git
//	    ref: v3                         # a tag or a commit
//	    path: rules.yml                 # in that repository; the default
//
// Every path is relative to the root of the repository whose file names it:
// this repository's (the folder of rules.yml) or the git import's.
//	disable: [HOUSE_PLURAL_COLLECTIONS] # imported rules this repository opts out of
//	rules:
//	  - id: HOUSE_RESOURCE_TYPE_DOMAIN
//	    on: resource
//	    check: type.endsWith('.acme.com')
//	    message: Resource types live under acme.com.
//	    doc: https://wiki.acme.com/api/resources
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the house rules file, next to buf.yaml.
const FileName = "rules.yml"

// EnvFile names a rules file to use instead of finding rules.yml.
const EnvFile = "DEDO_RULES"

// cacheDir is where git imports are fetched, under the root.
var cacheDir = filepath.Join(".tools", "rules")

// Kinds are the elements a rule can check.
var Kinds = []string{"file", "service", "method", "message", "field", "enum", "resource"}

var validID = regexp.MustCompile(`^HOUSE_[A-Z][A-Z0-9_]*$`)

// Rule is one house rule.
type Rule struct {
	ID      string `yaml:"id"`
	On      string `yaml:"on"`
	Check   string `yaml:"check"`
	Message string `yaml:"message"`
	Doc     string `yaml:"doc"`
	// Source is the rules file that defines the rule.
	Source string `yaml:"-"`
}

// Import is a rules file another one includes.
type Import struct {
	Path string `yaml:"path"`
	Git  string `yaml:"git"`
	Ref  string `yaml:"ref"`
}

type file struct {
	Imports []Import `yaml:"imports"`
	Disable []string `yaml:"disable"`
	Rules   []Rule   `yaml:"rules"`
}

// Find returns the rules file for a lint run started in dir: $DEDO_RULES,
// else rules.yml in the nearest directory up from dir that holds buf.yaml.
// It returns "" when there is none.
func Find(dir string) string {
	if p := os.Getenv(EnvFile); p != "" {
		return p
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "buf.yaml")); err == nil {
			p := filepath.Join(d, FileName)
			if _, err := os.Stat(p); err == nil {
				return p
			}
			return ""
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// Load reads a rules file and everything it imports. Git imports are read
// from the cache that Fetch fills, under the directory of the top file.
// Rules are returned in the order they appear, imports first.
func Load(path string) ([]Rule, error) {
	root := filepath.Dir(path)
	l := &loader{root: root, seen: map[string]bool{}, ids: map[string]string{}}
	rules, disabled, err := l.load(path, "", nil)
	if err != nil {
		return nil, err
	}
	for _, id := range disabled {
		if _, ok := l.ids[id]; !ok {
			return nil, fmt.Errorf("%s: disable names %s, which no rules file defines", path, id)
		}
	}
	out := rules[:0]
	for _, r := range rules {
		if !slices.Contains(disabled, r.ID) {
			out = append(out, r)
		}
	}
	return out, nil
}

type loader struct {
	root string
	seen map[string]bool
	ids  map[string]string // rule ID → the file that defines it
}

// load reads one rules file; repo is the git checkout it lies in, or "" for
// the service's own files, and stack holds the files importing it, to report
// import cycles.
func (l *loader) load(path, repo string, stack []string) (rules []Rule, disabled []string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	if slices.Contains(stack, abs) {
		return nil, nil, fmt.Errorf("rules files import each other: %s", strings.Join(append(stack, abs), " → "))
	}
	if l.seen[abs] {
		return nil, nil, nil // imported twice along different paths: once is enough
	}
	l.seen[abs] = true
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, err
	}
	var f file
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	// An empty rules file decodes to io.EOF: it simply has no rules.
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, imp := range f.Imports {
		target, targetRepo, err := resolve(l.root, abs, repo, imp)
		if err != nil {
			return nil, nil, err
		}
		if imp.Git != "" {
			if _, err := os.Stat(targetRepo); err != nil {
				return nil, nil, fmt.Errorf("%s: the import of %s@%s is not fetched yet: run dedo tools", abs, imp.Git, imp.Ref)
			}
		}
		r, d, err := l.load(target, targetRepo, append(stack, abs))
		if err != nil {
			return nil, nil, err
		}
		rules = append(rules, r...)
		disabled = append(disabled, d...)
	}
	for _, r := range f.Rules {
		r.Source = path
		if err := validate(r); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if prev, ok := l.ids[r.ID]; ok {
			return nil, nil, fmt.Errorf("%s: %s is defined twice, also in %s", path, r.ID, prev)
		}
		l.ids[r.ID] = path
		rules = append(rules, r)
	}
	return rules, append(disabled, f.Disable...), nil
}

// resolve returns the rules file an import names, and the git checkout it
// lies in ("" for the service's own files). A folder means its rules.yml.
//
// A path is relative to the root of the repository the importing file
// belongs to, wherever in it that file sits, so every file of a repository
// names the same file the same way. For the service's own files the root is
// the folder of its rules.yml, next to buf.yaml; there a path may also lead
// outside, such as ../platform-rules in a monorepo. For a git import the
// root is that repository's: the import's own path and every path its files
// import must stay inside it, symlinks followed, since a shared repository is
// someone else's code and must not make the lint read the service's files or
// the machine's.
func resolve(root, from, repo string, imp Import) (target, targetRepo string, err error) {
	switch {
	case imp.Git != "":
		if imp.Ref == "" {
			return "", "", fmt.Errorf("%s: the import of %s needs a ref: a tag or a commit, so every machine reads the same rules", from, imp.Git)
		}
		dir := CacheDir(root, imp)
		target, err := inRepo(dir, orDefault(imp.Path))
		if err != nil {
			return "", "", fmt.Errorf("%s: the import of %s@%s: %w", from, imp.Git, imp.Ref, err)
		}
		return target, dir, nil
	case imp.Path != "" && repo != "":
		target, err := inRepo(repo, imp.Path)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", from, err)
		}
		return target, repo, nil
	case imp.Path != "":
		return folderFile(filepath.Join(root, filepath.FromSlash(imp.Path))), "", nil
	}
	return "", "", fmt.Errorf("%s: an import needs a path or a git URL", from)
}

// inRepo resolves a path from the root of a git checkout, and refuses one
// that leads outside it.
func inRepo(repo, rel string) (string, error) {
	outside := fmt.Errorf("path %q is outside the repository it is imported from; paths there are relative to its root", rel)
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", outside
	}
	target := folderFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if !within(repo, target) {
		return "", outside
	}
	// A symlink in the checkout can point anywhere; judge where it leads.
	// Before dedo tools has fetched the repository there is nothing to
	// follow yet, and Load reports that instead.
	if real, err := filepath.EvalSymlinks(target); err == nil {
		realRepo, err := filepath.EvalSymlinks(repo)
		if err != nil || !within(realRepo, real) {
			return "", outside
		}
	}
	return target, nil
}

func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// folderFile turns a folder into the rules.yml inside it.
func folderFile(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, FileName)
	}
	return path
}

func orDefault(p string) string {
	if p == "" {
		return FileName
	}
	return p
}

func validate(r Rule) error {
	switch {
	case !validID.MatchString(r.ID):
		return fmt.Errorf("rule id %q must look like HOUSE_RESOURCE_TYPE_DOMAIN", r.ID)
	case !slices.Contains(Kinds, r.On):
		return fmt.Errorf("%s: on is %q, want one of %s", r.ID, r.On, strings.Join(Kinds, ", "))
	case strings.TrimSpace(r.Check) == "":
		return fmt.Errorf("%s: no check", r.ID)
	case strings.TrimSpace(r.Message) == "":
		return fmt.Errorf("%s: no message", r.ID)
	}
	return nil
}

// CacheDir is where a git import is fetched, under root.
func CacheDir(root string, imp Import) string {
	sum := sha256.Sum256([]byte(imp.Git + "@" + imp.Ref))
	return filepath.Join(root, cacheDir, hex.EncodeToString(sum[:])[:16])
}

// Fetch clones every git import reachable from the rules file at path into
// the cache, at its ref, and returns what it fetched. Imports already
// fetched are kept: a ref names one version.
func Fetch(path string) ([]string, error) {
	root := filepath.Dir(path)
	var fetched []string
	seen := map[string]bool{}
	var walk func(p, repo string) error
	walk = func(p, repo string) error {
		abs, err := filepath.Abs(p)
		if err != nil || seen[abs] {
			return err
		}
		seen[abs] = true
		b, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		var f file
		if err := yaml.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for _, imp := range f.Imports {
			if imp.Git != "" && imp.Ref != "" {
				dir := CacheDir(root, imp)
				if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
					if err := clone(imp, dir); err != nil {
						return err
					}
					fetched = append(fetched, imp.Git+"@"+imp.Ref)
				}
			}
			// Resolved after cloning, so symlinks in the checkout are judged.
			target, targetRepo, err := resolve(root, abs, repo, imp)
			if err != nil {
				return err
			}
			if err := walk(target, targetRepo); err != nil {
				return err
			}
		}
		return nil
	}
	return fetched, walk(path, "")
}

// clone checks out imp at its ref into dir, through a temporary directory,
// so an interrupted fetch leaves nothing half-done.
func clone(imp Import, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".fetch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, args := range [][]string{
		{"clone", "--quiet", "--no-checkout", imp.Git, tmp},
		{"-C", tmp, "checkout", "--quiet", imp.Ref},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("fetching %s@%s: git %s: %v\n%s", imp.Git, imp.Ref, args[0], err, out)
		}
	}
	return os.Rename(tmp, dir)
}
