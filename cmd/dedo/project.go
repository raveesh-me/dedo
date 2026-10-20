package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// project is what dedo init learns about a repository before changing it.
type project struct {
	dir       string
	module    string // the Go module path
	protoRoot string // the proto source root, relative to dir
	service   *service
	others    int // services beyond the first
	scopes    []string
	protos    int    // proto files found
	rootPkg   string // the Go package of the repository root, if it has one
	goPrefix  string // managed mode's go_package_prefix, if buf.gen.yaml sets one
	// schemaOnly marks a repository of protos without a Go module: it gets
	// the options module and the BSR lint plugin, and no Go parts.
	schemaOnly bool
	// undeclared lists services whose files do not import dedo's options.
	undeclared []string
	// tenancy is the tenant collection a service declares, such as
	// "locations", when the repository has a multi-tenant service.
	tenancy string
}

// service is the first service found, with the Go names that buf generate
// gives it under the managed mode dedo's buf.gen.yaml sets.
type service struct {
	Name     string // CounterService
	GoPkg    string // time_counterv1
	GoImport string // github.com/acme/time-counter/gen/go/acme/time_counter/v1
}

var (
	moduleLine = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	protoPkg   = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)\s*;`)
	protoSvc   = regexp.MustCompile(`(?m)^\s*service\s+(\w+)\s*\{`)
	nonIdent   = regexp.MustCompile(`[^a-zA-Z0-9_]`)
	goPackage  = regexp.MustCompile(`(?m)^\s*option\s+go_package\s*=\s*"([^"]+)"\s*;`)
	scopeLit   = regexp.MustCompile(`"([a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.(?:read|write))"`)
	tenancyLit = regexp.MustCompile(`tenancy\s*:\s*\{\s*collection\s*:\s*"([A-Za-z0-9]+)"`)
)

// skipDirs are never searched for proto sources.
var skipDirs = []string{".git", ".tools", "gen", "third_party", "node_modules", "vendor", "dist"}

func analyze(dir string) (*project, error) {
	p := &project{dir: dir}
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if found, _ := findProtos(dir); len(found) == 0 {
			return nil, fmt.Errorf("no go.mod and no .proto files in %s: for a Go service run go mod init first; for a schema repository add your protos first", dir)
		}
		p.schemaOnly = true
	case err != nil:
		return nil, err
	default:
		m := moduleLine.FindSubmatch(gomod)
		if m == nil {
			return nil, fmt.Errorf("%s: no module line", filepath.Join(dir, "go.mod"))
		}
		p.module = string(m[1])
	}
	protos, err := findProtos(dir)
	if err != nil {
		return nil, err
	}
	p.protos = len(protos)
	p.goPrefix = managedGoPrefix(dir)
	p.rootPkg = rootPackage(dir)
	p.protoRoot = bufModuleRoot(dir)
	if p.protoRoot == "" {
		p.protoRoot = guessRoot(dir, protos)
	}
	for _, rel := range protos {
		if err := p.read(rel); err != nil {
			return nil, err
		}
	}
	slices.Sort(p.scopes)
	p.scopes = slices.Compact(p.scopes)
	return p, nil
}

// read records a proto file's services and scopes.
func (p *project) read(rel string) error {
	src, err := os.ReadFile(filepath.Join(p.dir, rel))
	if err != nil {
		return err
	}
	for _, s := range scopeLit.FindAllSubmatch(src, -1) {
		p.scopes = append(p.scopes, string(s[1]))
	}
	if t := tenancyLit.FindSubmatch(src); t != nil && p.tenancy == "" {
		p.tenancy = string(t[1])
	}
	svcs := protoSvc.FindAllSubmatch(src, -1)
	pkg := protoPkg.FindSubmatch(src)
	if !strings.Contains(string(src), `"dedo/v1/options.proto"`) {
		for _, s := range svcs {
			p.undeclared = append(p.undeclared, string(s[1]))
		}
	}
	if len(svcs) == 0 || pkg == nil {
		return nil
	}
	if p.service != nil {
		p.others += len(svcs)
		return nil
	}
	p.others += len(svcs) - 1
	inRoot, err := filepath.Rel(p.protoRoot, rel)
	if err != nil || strings.HasPrefix(inRoot, "..") {
		inRoot = rel
	}
	inRoot = filepath.ToSlash(inRoot)
	p.service = &service{
		Name:     string(svcs[0][1]),
		GoPkg:    goPackageName(string(pkg[1])),
		GoImport: p.module + "/gen/go/" + filepath.ToSlash(filepath.Dir(inRoot)),
	}
	switch gp := goPackage.FindSubmatch(src); {
	case p.goPrefix != "":
		// Managed mode overrides go_package with its prefix.
		p.service.GoImport = p.goPrefix + "/" + filepath.ToSlash(filepath.Dir(inRoot))
	case gp != nil:
		// The file names its own Go package: "import/path;name" or
		// "import/path", whose last element protoc-gen-go then uses.
		importPath, name, ok := strings.Cut(string(gp[1]), ";")
		if !ok {
			name = nonIdent.ReplaceAllString(path.Base(importPath), "_")
		}
		p.service.GoImport, p.service.GoPkg = importPath, name
	}
	return nil
}

// managedGoPrefix returns the go_package_prefix that buf.gen.yaml's managed
// mode gives the repository's own files: the override without a module or
// path. It returns "" when managed mode is off or sets none.
func managedGoPrefix(dir string) string {
	doc, ok, err := loadYAML(filepath.Join(dir, "buf.gen.yaml"))
	if err != nil || !ok {
		return ""
	}
	managed := lookup(root(doc), "managed")
	if managed == nil || lookup(managed, "enabled") == nil || lookup(managed, "enabled").Value != "true" {
		return ""
	}
	for _, o := range lookup(managed, "override").Content {
		if lookup(o, "file_option") != nil && lookup(o, "file_option").Value == "go_package_prefix" &&
			lookup(o, "module") == nil && lookup(o, "path") == nil && lookup(o, "value") != nil {
			return lookup(o, "value").Value
		}
	}
	return ""
}

// goPackageName mirrors buf's managed mode: the last two package elements,
// so acme.time_counter.v1 becomes time_counterv1.
func goPackageName(pkg string) string {
	parts := strings.Split(pkg, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + parts[len(parts)-1]
	}
	return parts[0]
}

func findProtos(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != dir && slices.Contains(skipDirs, d.Name()) {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// bufModuleRoot returns the first module path of an existing buf.yaml.
func bufModuleRoot(dir string) string {
	doc, ok, err := loadYAML(filepath.Join(dir, "buf.yaml"))
	if err != nil || !ok {
		return ""
	}
	modules := lookup(root(doc), "modules")
	if modules == nil || len(modules.Content) == 0 {
		return ""
	}
	if path := lookup(modules.Content[0], "path"); path != nil {
		return path.Value
	}
	return ""
}

// guessRoot finds the directory that a proto file's package path is relative
// to: proto/acme/time_counter/v1/x.proto with package acme.time_counter.v1
// lives under proto. Without proto files it returns "proto".
func guessRoot(dir string, protos []string) string {
	for _, rel := range protos {
		src, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		pkg := protoPkg.FindSubmatch(src)
		if pkg == nil {
			continue
		}
		fileDir := filepath.ToSlash(filepath.Dir(rel))
		pkgDir := strings.ReplaceAll(string(pkg[1]), ".", "/")
		if fileDir == pkgDir {
			return "."
		}
		if root, ok := strings.CutSuffix(fileDir, "/"+pkgDir); ok {
			return root
		}
	}
	return "proto"
}

var goPackageLine = regexp.MustCompile(`(?m)^package\s+(\w+)`)

// rootPackage returns the package of the .go files in the repository root,
// or "" when it has none. Test files may use an _test package, so they are
// read only when nothing else is there.
func rootPackage(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	pkg := ""
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		m := goPackageLine.FindSubmatch(src)
		if m == nil {
			continue
		}
		found := strings.TrimSuffix(string(m[1]), "_test")
		if !strings.HasSuffix(name, "_test.go") {
			return found
		}
		pkg = found
	}
	return pkg
}
