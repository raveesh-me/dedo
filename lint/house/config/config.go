// Package config reads a repository's dedo.yaml, next to buf.yaml. Its lint
// and breaking sections say which of dedo's lint rules and breaking rules
// run, and hold the house rules of each kind, its own and those of the rules
// files it imports from folders, git repositories and HTTPS URLs; its
// annexure section names the guidance for coding agents that dedo adds to
// CLAUDE.md and AGENTS.md (see Annexure). It has no CEL in it, so the dedo
// command can fetch imports with it too.
//
// A shared rules file (rules.yml) holds the same, without annexure, and so
// did a repository's rules.yml before dedo.yaml, which is still read where
// there is no dedo.yaml:
//
//	imports:
//	  - path: platform-rules            # a folder (its rules.yml) or a file
//	  - git: https://github.com/acme/api-standards.git
//	    ref: v3                         # a tag or a commit
//	    path: rules.yml                 # in that repository; the default
//	  - url: https://acme.github.io/api-standards/v3/rules.yml
//	    sha256: 9f86d0…                 # the file's SHA-256, which dedo tools checks
//
// Every path is relative to the root of the repository whose file names it:
// this repository's (the folder of dedo.yaml), the git import's, or the
// folder of the url import's URL. An import brings both kinds of rules.
//
//	lint:
//	  use: [AIP_NOLANG, ZALANDO, DEDO, HOUSE] # dedo's rule sets, or rule IDs
//	  disable: [HOUSE_PLURAL_COLLECTIONS]     # rules this repository opts out of
//	  rules:
//	    - id: HOUSE_RESOURCE_TYPE_DOMAIN
//	      on: resource
//	      check: type.endsWith('.acme.com')
//	      message: Resource types live under acme.com.
//	      doc: https://wiki.acme.com/api/resources
//	      overrides: [HOUSE_RESOURCE_TYPE]    # rules this one replaces
//	      final: true                         # no rule may override or disable it
//	breaking:
//	  use: [DEDO, HOUSE]
//	  rules:
//	    - id: HOUSE_SCOPES_KEEP_PREFIX
//	      on: method
//	      check: against.scopes.all(s, scopes.exists(n, n.startsWith(s.split('.')[0])))
//	      message: An RPC keeps the apps its scopes name.
//
// A breaking rule checks an element against the one of the same name in the
// version buf breaking compares with, which its check reads as against.
//
// Only the repository's own file says use. disable names house rules or
// dedo's own, of its section's kind. A rule may override dedo's rules of its
// kind, and the rules of the files its own file imports, unless they are
// final; package rules checks that, and the names in use and disable, and
// applies them. Rule IDs are unique across both kinds.
package config

import (
	"bytes"
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

// FileName is the repository's dedo file, next to buf.yaml.
const FileName = "dedo.yaml"

// RulesFileName is a shared rules file, which a folder or git import means
// by default; a repository without dedo.yaml keeps its rules in one too.
const RulesFileName = "rules.yml"

// EnvFile names a dedo.yaml or rules file to use instead of finding one.
const EnvFile = "DEDO_RULES"

// cacheDir is where git imports are fetched, under the root.
var cacheDir = filepath.Join(".tools", "rules")

// Kinds are the elements a rule can check.
var Kinds = []string{"file", "service", "method", "message", "field", "enum", "resource"}

var (
	validID = regexp.MustCompile(`^HOUSE_[A-Z][A-Z0-9_]*$`)
	// anyID is the ID of a rule of any source, which an override names.
	anyID = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Rule is one house rule.
type Rule struct {
	// Breaking is set for a rule of a breaking section: buf breaking runs
	// it, on each element paired with its earlier version.
	Breaking bool   `yaml:"-" json:"breaking,omitempty"`
	ID       string `yaml:"id"`
	On       string `yaml:"on"`
	Check    string `yaml:"check"`
	Message  string `yaml:"message"`
	Doc      string `yaml:"doc"`
	// Overrides are the rules this one replaces: they no longer run.
	Overrides []string `yaml:"overrides"`
	// Final rules cannot be overridden or disabled, by any file.
	Final bool `yaml:"final"`
	// Source is the rules file that defines the rule, as it was named.
	Source string `yaml:"-"`
	// File is Source as an absolute path.
	File string `yaml:"-"`
	// Importers are the files that import File, directly or through
	// others, as absolute paths. Their rules outrank this one.
	Importers []string `yaml:"-"`
}

// Import is a rules file another one includes.
type Import struct {
	Path string `yaml:"path"`
	Git  string `yaml:"git"`
	Ref  string `yaml:"ref"`
	URL  string `yaml:"url"`
	// SHA256 pins a url import, and a path import of a url-imported file.
	SHA256 string `yaml:"sha256"`
}

// section is a file's lint or breaking section.
type section struct {
	Use     []string `yaml:"use"`
	Disable []string `yaml:"disable"`
	Rules   []Rule   `yaml:"rules"`
}

// file is a shared rules file, and the rules of a dedo.yaml.
type file struct {
	Imports  []Import  `yaml:"imports"`
	Lint     section   `yaml:"lint"`
	Breaking section   `yaml:"breaking"`
	Go       goSection `yaml:"go"`
}

// project is a dedo.yaml.
type project struct {
	file     `yaml:",inline"`
	Annexure []Annexure `yaml:"annexure"`
}

// IsProject reports whether path names a dedo.yaml rather than a rules file.
func IsProject(path string) bool {
	return filepath.Base(path) == FileName
}

// decode reads a dedo.yaml or a rules file; strict rejects unknown keys.
func decode(path string, b []byte, strict bool) (project, error) {
	if err := oldLayout(path, b); err != nil {
		return project{}, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(strict)
	var p project
	var err error
	if IsProject(path) {
		err = dec.Decode(&p)
	} else {
		err = dec.Decode(&p.file)
	}
	// An empty file decodes to io.EOF: it simply has no rules.
	if err != nil && !errors.Is(err, io.EOF) {
		return project{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// oldLayout explains a file written for a dedo before v0.32.1, whose
// dedo.yaml held one rules section, and whose rules files held its keys at
// the top, instead of lint and breaking sections.
func oldLayout(path string, b []byte) error {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(b, &top); err != nil {
		return nil // decode reports it
	}
	if IsProject(path) {
		if _, ok := top["rules"]; ok {
			return fmt.Errorf("%s: its rules section is now lint, with imports at the top of the file and a breaking section beside it: run dedo upgrade", path)
		}
		return nil
	}
	for _, key := range []string{"rules", "use", "disable"} {
		if _, ok := top[key]; ok {
			return fmt.Errorf("%s: %s belongs in a lint section now (or breaking, for breaking rules), with imports at the top of the file", path, key)
		}
	}
	return nil
}

// Find returns the file for a lint run started in dir: $DEDO_RULES, else
// dedo.yaml, or failing that rules.yml, in the nearest directory up from dir
// that holds buf.yaml. It returns "" when there is none.
func Find(dir string) string {
	if p := os.Getenv(EnvFile); p != "" {
		return p
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "buf.yaml")); err == nil {
			for _, name := range []string{FileName, RulesFileName} {
				if p := filepath.Join(d, name); exists(p) {
					return p
				}
			}
			return ""
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// Config is what a repository's rules say, imports included.
type Config struct {
	Lint     Selection `json:"lint"`
	Breaking Selection `json:"breaking"`
	// Go is what the go sections say, for dedo vet.
	Go GoSelection `json:"go"`
}

// Selection is what one kind of rules says: lint or breaking.
type Selection struct {
	// Use lists the rule sets and rules of dedo's that run; nil means
	// the rules do not say, so dedo's default runs.
	Use []string `json:"use,omitempty"`
	// Rules are the house rules, in the order they appear, imports first,
	// without the disabled ones.
	Rules []Rule `json:"rules"`
	// Disabled are the IDs disable names: house rules, removed from Rules,
	// and dedo's own, which package rules turns off.
	Disabled []string `json:"disabled"`
}

// Load reads a rules file and everything it imports. Git and url imports are
// read from the cache that Fetch fills, under the directory of the top file.
func Load(path string) (Config, error) {
	root := filepath.Dir(path)
	l := &loader{root: root, seen: map[string]bool{}, ids: map[string]string{}, imports: map[string][]string{}}
	all, err := l.load(path, "", nil)
	if err != nil {
		return Config{}, err
	}
	importers := l.importers()
	lint, err := l.selectKind(path, "lint", all.lint, all.lintDisabled, l.lintUse, importers)
	if err != nil {
		return Config{}, err
	}
	breaking, err := l.selectKind(path, "breaking", all.breaking, all.breakingDisabled, l.breakingUse, importers)
	if err != nil {
		return Config{}, err
	}
	golang, err := l.selectGo(path, all.goRules, all.goDisabled, importers)
	if err != nil {
		return Config{}, err
	}
	return Config{Lint: lint, Breaking: breaking, Go: golang}, nil
}

// selectKind checks one kind's disable against its rules and returns what
// runs of it.
func (l *loader) selectKind(path, kind string, rules []Rule, disabled, use []string, importers map[string][]string) (Selection, error) {
	for _, id := range disabled {
		if !IsHouse(id) {
			continue
		}
		if !slices.ContainsFunc(rules, func(r Rule) bool { return r.ID == id }) {
			if _, ok := l.ids[id]; ok {
				return Selection{}, fmt.Errorf("%s: %s.disable names %s, which is not a %s rule", path, kind, id, kind)
			}
			return Selection{}, fmt.Errorf("%s: %s.disable names %s, which no rules file defines", path, kind, id)
		}
	}
	for _, r := range rules {
		if r.Final && slices.Contains(disabled, r.ID) {
			return Selection{}, fmt.Errorf("%s: %s.disable names %s, which %s marks final; keep it, or ask its owners to drop final", path, kind, r.ID, r.Source)
		}
	}
	s := Selection{Use: use, Disabled: disabled}
	for _, r := range rules {
		if !slices.Contains(disabled, r.ID) {
			r.Importers = importers[r.File]
			s.Rules = append(s.Rules, r)
		}
	}
	return s, nil
}

// loaded is what a rules file and its imports hold, by kind.
type loaded struct {
	lint, breaking                 []Rule
	lintDisabled, breakingDisabled []string
	goRules                        []GoRule
	goDisabled                     []string
}

func (a *loaded) add(b loaded) {
	a.lint = append(a.lint, b.lint...)
	a.breaking = append(a.breaking, b.breaking...)
	a.lintDisabled = append(a.lintDisabled, b.lintDisabled...)
	a.breakingDisabled = append(a.breakingDisabled, b.breakingDisabled...)
	a.goRules = append(a.goRules, b.goRules...)
	a.goDisabled = append(a.goDisabled, b.goDisabled...)
}

// IsHouse reports whether id names a house rule rather than one of dedo's.
func IsHouse(id string) bool {
	return validID.MatchString(id)
}

type loader struct {
	// lintUse, breakingUse and goUse are the use of the top file's sections.
	lintUse, breakingUse, goUse []string
	root                        string
	seen                        map[string]bool
	ids                         map[string]string   // rule ID → the file that defines it
	imports                     map[string][]string // file → the files it imports
}

// importers maps each file to every file that imports it, directly or
// through others, sorted.
func (l *loader) importers() map[string][]string {
	out := map[string][]string{}
	var visit func(from, file string)
	visit = func(from, file string) {
		if slices.Contains(out[file], from) {
			return
		}
		out[file] = append(out[file], from)
		for _, next := range l.imports[file] {
			visit(from, next)
		}
	}
	for from, files := range l.imports {
		for _, f := range files {
			visit(from, f)
		}
	}
	for _, files := range out {
		slices.Sort(files)
	}
	return out
}

// load reads one rules file; repo is the git checkout it lies in, or "" for
// the service's own files, and stack holds the files importing it, to report
// import cycles.
func (l *loader) load(path, repo string, stack []string) (loaded, error) {
	var out loaded
	abs, err := filepath.Abs(path)
	if err != nil {
		return out, err
	}
	if slices.Contains(stack, abs) {
		return out, fmt.Errorf("rules files import each other: %s", strings.Join(append(stack, abs), " → "))
	}
	if l.seen[abs] {
		return out, nil // imported twice along different paths: once is enough
	}
	l.seen[abs] = true
	b, err := os.ReadFile(abs)
	if err != nil {
		return out, err
	}
	p, err := decode(path, b, true)
	if err != nil {
		return out, err
	}
	f := p.file
	for _, k := range []struct {
		kind string
		sec  section
	}{{"lint", f.Lint}, {"breaking", f.Breaking}} {
		kind, sec := k.kind, k.sec
		if sec.Use != nil && len(stack) > 0 {
			return out, fmt.Errorf("%s: %s.use belongs in the repository's own dedo.yaml, which chooses the rules it runs; imported files add rules", path, kind)
		}
		for _, id := range sec.Disable {
			if !anyID.MatchString(id) {
				return out, fmt.Errorf("%s: %s.disable names %q, which is not a rule ID such as HOUSE_PLURAL_COLLECTIONS", path, kind, id)
			}
		}
	}
	if f.Go.Use != nil && len(stack) > 0 {
		return out, fmt.Errorf("%s: go.use belongs in the repository's own dedo.yaml, which chooses the rules it runs; imported files add rules", path)
	}
	if len(stack) == 0 {
		l.lintUse, l.breakingUse, l.goUse = f.Lint.Use, f.Breaking.Use, f.Go.Use
	}
	for _, imp := range f.Imports {
		target, targetRepo, err := resolve(l.root, abs, repo, imp)
		if err != nil {
			return out, err
		}
		if imp.Git != "" {
			if _, err := os.Stat(targetRepo); err != nil {
				return out, fmt.Errorf("%s: the import of %s@%s is not fetched yet: run dedo tools", abs, imp.Git, imp.Ref)
			}
		}
		if overHTTPS(imp, repo) {
			if err := checkPin(abs, importName(imp), imp.SHA256); err != nil {
				return out, err
			}
			if _, err := os.Stat(target); err != nil {
				return out, fmt.Errorf("%s: the import of %s is not fetched yet: run dedo tools", abs, importName(imp))
			}
		}
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return out, err
		}
		// Recorded before load, which skips a file it has read already, so
		// a file imported along two paths ranks below both importers.
		l.imports[abs] = append(l.imports[abs], targetAbs)
		got, err := l.load(target, targetRepo, append(stack, abs))
		if err != nil {
			return out, err
		}
		out.add(got)
	}
	for _, kind := range []struct {
		breaking bool
		rules    []Rule
		into     *[]Rule
	}{{false, f.Lint.Rules, &out.lint}, {true, f.Breaking.Rules, &out.breaking}} {
		for _, r := range kind.rules {
			r.Breaking = kind.breaking
			r.Source = path
			r.File = abs
			r.Doc = resolveDoc(r.Doc, repo, abs)
			if err := validate(r); err != nil {
				return out, fmt.Errorf("%s: %w", path, err)
			}
			if prev, ok := l.ids[r.ID]; ok {
				return out, fmt.Errorf("%s: %s is defined twice, also in %s", path, r.ID, prev)
			}
			l.ids[r.ID] = path
			*kind.into = append(*kind.into, r)
		}
	}
	out.lintDisabled = append(out.lintDisabled, f.Lint.Disable...)
	out.breakingDisabled = append(out.breakingDisabled, f.Breaking.Disable...)
	if err := l.loadGo(path, abs, repo, f.Go, &out); err != nil {
		return out, err
	}
	return out, nil
}

// resolve returns the rules file an import names, and the git checkout or
// url import cache it lies in ("" for the service's own files). A folder
// means its rules.yml.
//
// A path is relative to the root of the repository the importing file
// belongs to, wherever in it that file sits, so every file of a repository
// names the same file the same way. For the service's own files the root is
// the folder of its dedo.yaml, next to buf.yaml; there a path may also lead
// outside, such as ../platform-rules in a monorepo. For a git import the
// root is that repository's: the import's own path and every path its files
// import must stay inside it, symlinks followed, since a shared repository is
// someone else's code and must not make the lint read the service's files or
// the machine's.
func resolve(root, from, repo string, imp Import) (target, targetRepo string, err error) {
	switch {
	case imp.Git != "" && imp.URL != "":
		return "", "", fmt.Errorf("%s: an import names a git repository or a url, not both", from)
	case imp.URL != "":
		_, name, err := rootURL(imp.URL)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", from, err)
		}
		dir := CacheDir(root, imp)
		return filepath.Join(dir, name), dir, nil
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
	return "", "", fmt.Errorf("%s: an import needs a path, a git repository or a url", from)
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
		return filepath.Join(path, RulesFileName)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func orDefault(p string) string {
	if p == "" {
		return RulesFileName
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
	return validOverrides(r.ID, r.Overrides)
}

// CacheDir is where a git or url import is fetched, under root.
func CacheDir(root string, imp Import) string {
	key := imp.Git + "@" + imp.Ref
	if imp.URL != "" {
		key = imp.URL + "#" + strings.ToLower(imp.SHA256)
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(root, cacheDir, hex.EncodeToString(sum[:])[:16])
}

// Fetch clones every git import reachable from the rules file at path into
// the cache, at its ref, downloads every url import and the files those
// import, checking each against its sha256, and returns what it fetched.
// Imports already fetched are kept: a ref or a sha256 names one version.
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
		proj, err := decode(p, b, false)
		if err != nil {
			return err
		}
		for _, imp := range proj.Imports {
			if imp.URL != "" && imp.Git == "" {
				dir := CacheDir(root, imp)
				if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
					if err := fetchURL(imp, dir); err != nil {
						return err
					}
					fetched = append(fetched, imp.URL)
				}
			}
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
			if imp.URL == "" && overHTTPS(imp, repo) {
				// A path import of a url-imported file is fetched from beside it.
				if err := checkPin(abs, importName(imp), imp.SHA256); err != nil {
					return err
				}
				got, err := fetchBeside(repo, target, imp.SHA256)
				if err != nil {
					return err
				}
				if got {
					u, _ := fileURL(repo, target)
					fetched = append(fetched, u.String())
				}
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

// importName names an import in messages.
func importName(imp Import) string {
	switch {
	case imp.URL != "":
		return imp.URL
	case imp.Git != "":
		return imp.Git + "@" + imp.Ref
	}
	return imp.Path
}
