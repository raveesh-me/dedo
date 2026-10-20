package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	scopeLit   = regexp.MustCompile(`"([a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.(?:read|write))"`)
)

// skipDirs are never searched for proto sources.
var skipDirs = []string{".git", ".tools", "gen", "third_party", "node_modules", "vendor", "dist"}

func analyze(dir string) (*project, error) {
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no go.mod in %s: run go mod init first", dir)
	}
	if err != nil {
		return nil, err
	}
	m := moduleLine.FindSubmatch(gomod)
	if m == nil {
		return nil, fmt.Errorf("%s: no module line", filepath.Join(dir, "go.mod"))
	}
	p := &project{dir: dir, module: string(m[1])}
	protos, err := findProtos(dir)
	if err != nil {
		return nil, err
	}
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
	svcs := protoSvc.FindAllSubmatch(src, -1)
	pkg := protoPkg.FindSubmatch(src)
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
	return nil
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
