package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// bufModule is a module buf.yaml lists: its directory, relative to the
// repository and slash-separated, and its BSR name, which only a published
// module has.
type bufModule struct {
	Path, Name string
}

// bufModules returns the modules of the repository's buf.yaml, in its
// order, or none when there is no buf.yaml.
func bufModules(dir string) ([]bufModule, error) {
	doc, ok, err := loadYAML(filepath.Join(dir, "buf.yaml"))
	if err != nil || !ok {
		return nil, err
	}
	modules := lookup(root(doc), "modules")
	if modules == nil {
		return nil, nil
	}
	var out []bufModule
	for _, m := range modules.Content {
		p := lookup(m, "path")
		if p == nil {
			continue
		}
		mod := bufModule{Path: path.Clean(p.Value)}
		if n := lookup(m, "name"); n != nil {
			mod.Name = n.Value
		}
		out = append(out, mod)
	}
	return out, nil
}

// hasGoMod reports whether dir holds a go.mod: it is a Go module of its own.
func hasGoMod(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// nestedGoModules returns the directories below dir, relative and
// slash-separated, that hold a Go module of their own, such as a backend
// for frontend's bff/. The repository's go tooling (./...) leaves them out,
// and so do dedo modules and dedo init. It does not look in hidden,
// vendored or generated directories.
func nestedGoModules(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == dir {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || slices.Contains(skipDirs, name) || walkSkip[name] {
			return filepath.SkipDir
		}
		if hasGoMod(p) {
			if rel, err := filepath.Rel(dir, p); err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	return out
}

// insideModule returns the innermost of the nested Go modules that holds
// rel, a slash-separated path relative to the repository.
func insideModule(rel string, nested []string) (string, bool) {
	found := ""
	for _, n := range nested {
		if (rel == n || strings.HasPrefix(rel, n+"/")) && len(n) > len(found) {
			found = n
		}
	}
	return found, found != ""
}
