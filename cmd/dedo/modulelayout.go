package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Layout is where a service repository keeps its modules, as
// guidance/domains.md lays them out. A module is a proto package's module,
// <context>.<m>.v<n>: its Go is internal/<m>/** and the code generated from
// its packages (every version of a module is the same module). Shared code
// is internal/shared/** and the code generated from the context's
// unversioned packages, such as <context>.type, and imports no module. The
// composition roots, the main packages and app/**, are the only code that
// imports several modules.
type Layout struct {
	// Module is the Go module path, such as github.com/acme/shop.
	Module string
	// Contexts are the proto package prefixes before <m>.v<n>, such as
	// acme.shop; "" for a package <m>.v<n>.
	Contexts []string
	// Modules are the modules, sorted.
	Modules []string
	// Generated maps the import path of a package generated from the
	// repository's protos to its module, or to "" when it is generated from
	// a shared proto package.
	Generated map[string]string
	// Mains are the import paths of the main packages.
	Mains map[string]bool
	// versions are each module's proto versions, such as v1 and v2.
	versions map[string][]string
	// contextDirs are the contexts as directories: acme/shop.
	contextDirs []string
}

// Shared is the reserved name of what every module uses. No proto module
// may be called shared.
const Shared = "shared"

// Kind is what a Go package is to the layout.
type Kind int

const (
	// KindOther is a package the rules leave alone, such as docs/ or a tool.
	KindOther Kind = iota
	// KindModule belongs to one module.
	KindModule
	// KindShared imports no module.
	KindShared
	// KindRoot is a composition root: a main package, or app/**.
	KindRoot
)

// protoVersion matches a proto package's version element: v1, v2beta1.
var protoVersion = regexp.MustCompile(`^v[0-9]+((alpha|beta|test)[0-9]*)?$`)

// protoModule returns the context and module of a versioned proto package:
// acme.shop.orders.v1 is module orders of context acme.shop. ok is false
// for an unversioned, shared package, such as acme.shop.type.
func protoModule(pkg string) (context, module, version string, ok bool) {
	parts := strings.Split(pkg, ".")
	n := len(parts)
	if n < 2 || !protoVersion.MatchString(parts[n-1]) {
		return "", "", "", false
	}
	return strings.Join(parts[:n-2], "."), parts[n-2], parts[n-1], true
}

// addPackage records a proto package of the repository's own.
func (l *Layout) addPackage(pkg string) {
	context, module, version, ok := protoModule(pkg)
	if !ok {
		return
	}
	if !slices.Contains(l.Contexts, context) {
		l.Contexts = append(l.Contexts, context)
		slices.Sort(l.Contexts)
		if context != "" {
			l.contextDirs = append(l.contextDirs, strings.ReplaceAll(context, ".", "/"))
		}
	}
	if !slices.Contains(l.Modules, module) {
		l.Modules = append(l.Modules, module)
		slices.Sort(l.Modules)
	}
	if l.versions == nil {
		l.versions = map[string][]string{}
	}
	if !slices.Contains(l.versions[module], version) {
		l.versions[module] = append(l.versions[module], version)
	}
}

// Classify returns what the package at importPath is, and its module.
func (l Layout) Classify(importPath string) (Kind, string) {
	rel, ok := strings.CutPrefix(importPath, l.Module)
	if !ok || (rel != "" && rel[0] != '/') {
		return KindOther, ""
	}
	rel = strings.TrimPrefix(rel, "/")
	parts := strings.Split(rel, "/")
	switch {
	case rel == "" || parts[0] == "app":
		return KindRoot, ""
	case parts[0] == "internal" && len(parts) > 1 && parts[1] == Shared:
		return KindShared, ""
	case parts[0] == "internal" && len(parts) > 1 && slices.Contains(l.Modules, parts[1]):
		return KindModule, parts[1]
	}
	if module, ok := l.Generated[importPath]; ok {
		if module == "" {
			return KindShared, ""
		}
		return KindModule, module
	}
	if l.Mains[importPath] {
		return KindRoot, ""
	}
	return KindOther, ""
}

// Dirs checks the directory layout: no proto module is called shared,
// internal/ holds only modules and shared, and no module's code lives
// anywhere but internal/<m>, beside its protos and in code generated from
// them. dirs are the repository's directories, relative and
// slash-separated.
func (l Layout) Dirs(dirs []string) []string {
	var out []string
	if slices.Contains(l.Modules, Shared) {
		out = append(out, fmt.Sprintf("a proto package names module %q, which is reserved for shared code: rename the package", Shared))
	}
	has := map[string]bool{}
	for _, d := range dirs {
		has[d] = true
	}
	for _, d := range dirs {
		parent, name := path.Split(d)
		parent = strings.TrimSuffix(parent, "/")
		switch {
		case parent == "internal" && name != Shared && !slices.Contains(l.Modules, name):
			out = append(out, fmt.Sprintf("%s: internal/ holds only modules (proto packages <context>.<m>.v<n>) and %s", d, Shared))
		case slices.Contains(l.Modules, name) && !l.moduleHome(parent, d, has):
			out = append(out, fmt.Sprintf("%s: module %s lives in internal/%s, nowhere else", d, name, name))
		}
	}
	return out
}

// moduleHome reports whether dir, named after a module, may sit in parent:
// internal/, a module's own subpackage (internal/m/migrations), or a tree
// that mirrors the proto packages, such as proto/acme/shop/m or
// gen/go/acme/shop/m, which ends in the context or holds the module's
// versions.
func (l Layout) moduleHome(parent, dir string, has map[string]bool) bool {
	if parent == "internal" || strings.HasPrefix(parent, "internal/") {
		return true
	}
	for _, c := range l.contextDirs {
		if parent == c || strings.HasSuffix(parent, "/"+c) {
			return true
		}
	}
	for _, v := range l.versions[path.Base(dir)] {
		if has[dir+"/"+v] {
			return true
		}
	}
	return false
}
