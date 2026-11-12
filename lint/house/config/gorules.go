package config

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// A file's go section holds house Go rules, which dedo vet runs on the Go
// code of the service, beside dedo's own DEDO_GO_* rules:
//
//	go:
//	  use: [DEDO, HOUSE]                       # only in the repository's own dedo.yaml
//	  disable: [DEDO_GO_EVENT_DECLARED]
//	  rules:
//	    - id: HOUSE_GO_NO_PLATFORM_SDK
//	      forbid_import: example.com/platform-sdk/...
//	      allow_in: [internal/legacy/**]
//	      message: Call the platform services directly.
//	      doc: https://wiki.example.com/go/platform-sdk
//	      overrides: [HOUSE_GO_NO_SDK]           # rules this one replaces
//	      final: true                           # no rule may override or disable it
//
// Imports bring Go rules as they bring lint and breaking ones, and the same
// order of layers, overrides and final apply among them; package gorules
// checks the overrides against dedo's Go rules.

// goID is the ID of a house Go rule.
var goID = regexp.MustCompile(`^HOUSE_GO_[A-Z][A-Z0-9_]*$`)

// importPath is a Go import path, or one followed by /... for every package
// under it.
var importPath = regexp.MustCompile(`^[A-Za-z0-9_.~+-]+(/[A-Za-z0-9_.~+-]+)*(/\.\.\.)?$`)

// GoRule is one house Go rule. Its kind is the one field of its kind that is
// set; forbid_import is the only kind so far.
type GoRule struct {
	ID string `yaml:"id" json:"id"`
	// ForbidImport is an import path the code may not import, or a path
	// followed by /... for it and every package under it.
	ForbidImport string `yaml:"forbid_import" json:"forbid_import"`
	// AllowIn are globs of the files, relative to the folder of dedo.yaml,
	// that may import it all the same. ** matches any number of folders.
	AllowIn []string `yaml:"allow_in" json:"allow_in,omitempty"`
	Message string   `yaml:"message" json:"message"`
	Doc     string   `yaml:"doc" json:"doc,omitempty"`
	// Overrides are the rules this one replaces: they no longer run.
	Overrides []string `yaml:"overrides" json:"overrides,omitempty"`
	// Final rules cannot be overridden or disabled, by any file.
	Final bool `yaml:"final" json:"final,omitempty"`
	// Source is the rules file that defines the rule, as it was named.
	Source string `yaml:"-" json:"source,omitempty"`
	// File is Source as an absolute path.
	File string `yaml:"-" json:"file,omitempty"`
	// Importers are the files that import File, directly or through
	// others, as absolute paths. Their rules outrank this one.
	Importers []string `yaml:"-" json:"importers,omitempty"`
}

// goSection is a file's go section.
type goSection struct {
	Use     []string `yaml:"use"`
	Disable []string `yaml:"disable"`
	Rules   []GoRule `yaml:"rules"`
}

// GoSelection is what the go sections say: the house Go rules, without the
// disabled ones, and the use and disable that choose among dedo's.
type GoSelection struct {
	// Use lists the rule sets (DEDO, HOUSE) and rules that run; nil
	// means the rules do not say, so both sets run.
	Use      []string `json:"use,omitempty"`
	Rules    []GoRule `json:"rules,omitempty"`
	Disabled []string `json:"disabled,omitempty"`
}

// Forbids reports whether the rule forbids importing pkg.
func (r GoRule) Forbids(pkg string) bool {
	if prefix, ok := strings.CutSuffix(r.ForbidImport, "/..."); ok {
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	}
	return pkg == r.ForbidImport
}

// Allows reports whether file, a slash-separated path relative to the
// folder of dedo.yaml, may import what the rule forbids.
func (r GoRule) Allows(file string) bool {
	return slices.ContainsFunc(r.AllowIn, func(glob string) bool { return globMatch(glob, file) })
}

// globMatch matches a slash-separated path against a glob whose ** element
// matches any number of path elements, none included, and whose other
// elements match one element each, as path.Match does.
func globMatch(glob, name string) bool {
	return matchParts(strings.Split(glob, "/"), strings.Split(name, "/"))
}

func matchParts(glob, name []string) bool {
	if len(glob) == 0 {
		return len(name) == 0
	}
	if glob[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchParts(glob[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(glob[0], name[0])
	return err == nil && ok && matchParts(glob[1:], name[1:])
}

// validateGo checks one house Go rule.
func validateGo(r GoRule) error {
	switch {
	case !goID.MatchString(r.ID):
		return fmt.Errorf("go rule id %q must look like HOUSE_GO_NO_PLATFORM_SDK", r.ID)
	case r.ForbidImport == "":
		return fmt.Errorf("%s: no forbid_import, the import path the rule forbids (the only kind of go rule so far)", r.ID)
	case !validImportPath(r.ForbidImport):
		return fmt.Errorf("%s: forbid_import %q is not an import path, such as example.com/sdk or example.com/sdk/...", r.ID, r.ForbidImport)
	case strings.TrimSpace(r.Message) == "":
		return fmt.Errorf("%s: no message", r.ID)
	}
	for _, glob := range r.AllowIn {
		if err := validGlob(glob); err != nil {
			return fmt.Errorf("%s: allow_in %q: %w", r.ID, glob, err)
		}
	}
	return validOverrides(r.ID, r.Overrides)
}

// validImportPath reports whether p is an import path, optionally followed
// by /..., with no element made of dots alone.
func validImportPath(p string) bool {
	if !importPath.MatchString(p) {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(p, "/..."), "/") {
		if strings.Trim(part, ".") == "" {
			return false
		}
	}
	return true
}

// validGlob checks an allow_in glob: relative, and each element a pattern
// path.Match accepts.
func validGlob(glob string) error {
	if glob == "" || strings.HasPrefix(glob, "/") {
		return fmt.Errorf("want a glob relative to the folder of dedo.yaml, such as internal/legacy/**")
	}
	for _, part := range strings.Split(glob, "/") {
		if _, err := path.Match(part, ""); err != nil {
			return err
		}
	}
	return nil
}

func validOverrides(id string, overrides []string) error {
	for _, o := range overrides {
		switch {
		case !anyID.MatchString(o):
			return fmt.Errorf("%s: overrides %q, which is not a rule ID such as DEDO_GO_EVENT_DECLARED", id, o)
		case o == id:
			return fmt.Errorf("%s: overrides itself", id)
		}
	}
	return nil
}

// selectGo checks the go sections' disable against the Go rules and
// returns what runs of them.
func (l *loader) selectGo(path string, rules []GoRule, disabled []string, importers map[string][]string) (GoSelection, error) {
	for _, id := range disabled {
		if !IsHouse(id) || slices.ContainsFunc(rules, func(r GoRule) bool { return r.ID == id }) {
			continue
		}
		if _, ok := l.ids[id]; ok {
			return GoSelection{}, fmt.Errorf("%s: go.disable names %s, which is not a go rule", path, id)
		}
		return GoSelection{}, fmt.Errorf("%s: go.disable names %s, which no rules file defines", path, id)
	}
	s := GoSelection{Use: l.goUse, Disabled: disabled}
	for _, r := range rules {
		if !slices.Contains(disabled, r.ID) {
			r.Importers = importers[r.File]
			s.Rules = append(s.Rules, r)
			continue
		}
		if r.Final {
			return GoSelection{}, fmt.Errorf("%s: go.disable names %s, which %s marks final; keep it, or ask its owners to drop final", path, r.ID, r.Source)
		}
	}
	return s, nil
}

// loadGo adds a file's Go rules, checked, to out.
func (l *loader) loadGo(path, abs, repo string, sec goSection, out *loaded) error {
	for _, id := range sec.Disable {
		if !anyID.MatchString(id) {
			return fmt.Errorf("%s: go.disable names %q, which is not a rule ID such as HOUSE_GO_NO_PLATFORM_SDK", path, id)
		}
	}
	for _, r := range sec.Rules {
		r.Source = path
		r.File = abs
		r.Doc = resolveDoc(r.Doc, repo, abs)
		if err := validateGo(r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if prev, ok := l.ids[r.ID]; ok {
			return fmt.Errorf("%s: %s is defined twice, also in %s", path, r.ID, prev)
		}
		l.ids[r.ID] = path
		out.goRules = append(out.goRules, r)
	}
	out.goDisabled = append(out.goDisabled, sec.Disable...)
	return nil
}
