package gorules

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// HouseConfig is what the house analyzer runs: the house Go rules that run,
// and the folder of dedo.yaml, which their allow_in globs are relative to.
// dedo vet passes it as the analyzer's -config flag, in JSON, so go vet's
// cache tells apart runs with different rules.
type HouseConfig struct {
	Root  string          `json:"root"`
	Rules []config.GoRule `json:"rules"`
}

// House runs the house Go rules its -config flag holds.
var House = &analysis.Analyzer{
	Name: "house_go",
	Doc:  "runs the HOUSE_GO_* rules of dedo.yaml and the rules files it imports",
	Run:  runHouse,
}

// houseFlag holds the house analyzer's -config flag.
var houseFlag string

func init() {
	House.Flags.StringVar(&houseFlag, "config", "", "the house Go rules to run, as JSON: {root, rules}")
}

// Flag returns the -config value that runs these rules.
func (c HouseConfig) Flag() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("house Go rules: %w", err)
	}
	return string(b), nil
}

func parseHouseFlag(s string) (HouseConfig, error) {
	var c HouseConfig
	if s == "" {
		return c, nil
	}
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, fmt.Errorf("house_go -config: %w", err)
	}
	return c, nil
}

func runHouse(pass *analysis.Pass) (any, error) {
	c, err := parseHouseFlag(houseFlag)
	if err != nil || len(c.Rules) == 0 {
		return nil, err
	}
	r := newReporter(pass)
	for _, f := range pass.Files {
		file := relFile(c.Root, pass.Fset.File(f.Pos()).Name())
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			for _, rule := range c.Rules {
				if rule.Forbids(path) && !rule.Allows(file) {
					r.report(spec.Pos(), rule.ID, rule.Doc, "%s imports %s: %s", file, path, rule.Message)
				}
			}
		}
	}
	return nil, nil
}

// relFile is a file's path relative to root, with slashes, or the path as
// it is when it lies outside root. Symbolic links are resolved when the
// path as given is not under root, as /tmp is /private/tmp on macOS.
func relFile(root, name string) string {
	if root == "" {
		return filepath.ToSlash(name)
	}
	if rel, ok := under(root, name); ok {
		return rel
	}
	realRoot, err1 := filepath.EvalSymlinks(root)
	realName, err2 := filepath.EvalSymlinks(name)
	if err1 == nil && err2 == nil {
		if rel, ok := under(realRoot, realName); ok {
			return rel
		}
	}
	return filepath.ToSlash(name)
}

func under(root, name string) (string, bool) {
	rel, err := filepath.Rel(root, name)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
