package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// modules checks that a service repository keeps its modules apart, in Go
// imports and in SQL, and prints their dependency graph: dedo modules,
// which make check runs. The modules are the repository's versioned proto
// packages, <context>.<m>.v<n>; guidance/domains.md lays out where each
// one's code lives.
//
// Events are not checked here: a topic belongs to its publisher, so a
// subscriber imports the publisher's package to name it, and Build counts
// that import as a dependency like any other. Which calls declare, publish
// or subscribe to a topic depends on the repository's own outbox, which
// dedo does not ship, so a repository that wants those calls checked
// checks them itself.
func modules(dir string, out io.Writer) error {
	dir, err := realPath(dir)
	if err != nil {
		return err
	}
	l, err := loadLayout(dir)
	if err != nil {
		return err
	}
	if len(l.Modules) == 0 {
		fmt.Fprintln(out, "dedo modules: no modules; a module is a versioned proto package, <context>.<m>.v<n>")
		return nil
	}
	pkgs, err := listPackages(dir, l.Module)
	if err != nil {
		return err
	}
	l.Generated, l.Mains = generatedPackages(pkgs, protoFiles(dir))
	dirs, sql, err := walkRepo(dir, l)
	if err != nil {
		return err
	}
	goSQL, err := packageSQL(dir, l, pkgs)
	if err != nil {
		return err
	}
	graph, findings := l.Build(imports(pkgs))
	findings = append(findings, l.Dirs(dirs)...)
	findings = append(findings, graph.Cycles()...)
	findings = append(findings, l.SQL(append(sql, goSQL...))...)
	if len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(out, f)
		}
		return fmt.Errorf("modules: %d finding(s); guidance/domains.md (dedo templates domains.md) says where each module's code lives", len(findings))
	}
	in := ""
	if c := strings.Join(l.Contexts, ", "); c != "" {
		in = " in " + c
	}
	count := fmt.Sprintf("%d modules", len(l.Modules))
	if len(l.Modules) == 1 {
		count = "1 module"
	}
	fmt.Fprintf(out, "dedo modules: %s%s, no cycles; each depends on:\n%s", count, in, graph)
	return nil
}

// loadLayout reads the Go module path from go.mod and the modules from the
// repository's proto packages, wherever buf.yaml's modules keep them.
func loadLayout(dir string) (Layout, error) {
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return Layout{}, fmt.Errorf("dedo modules runs in a Go service repository: %w", err)
	}
	m := moduleLine.FindSubmatch(gomod)
	if m == nil {
		return Layout{}, fmt.Errorf("%s: no module line", filepath.Join(dir, "go.mod"))
	}
	l := Layout{Module: string(m[1])}
	files := protoFiles(dir)
	for _, f := range files {
		if f.pkg != "" {
			l.addPackage(f.pkg)
		}
	}
	return l, nil
}

// protoFile is one of the repository's own .proto files.
type protoFile struct {
	rel, pkg string
}

// protoFiles lists the repository's own .proto files with their packages,
// leaving out vendored ones (third_party/) and generated trees.
func protoFiles(dir string) []protoFile {
	rels, err := findProtos(dir)
	if err != nil {
		return nil
	}
	var out []protoFile
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		f := protoFile{rel: filepath.ToSlash(rel)}
		if pkg := protoPkg.FindSubmatch(src); pkg != nil {
			f.pkg = string(pkg[1])
		}
		out = append(out, f)
	}
	return out
}

// listedPackage is what go list says of a package.
type listedPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	Imports    []string
	Module     *struct{ Path string }
}

// listPackages lists the Go module's packages with their non-test imports,
// through go list -deps, which loads the whole import graph.
func listPackages(dir, module string) ([]listedPackage, error) {
	cmd := exec.Command("go", "list", "-deps", "-json=ImportPath,Name,Dir,GoFiles,Imports,Module", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(stdout))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		if p.Module != nil && p.Module.Path == module {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, nil
}

func imports(pkgs []listedPackage) []Package {
	out := make([]Package, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, Package{Path: p.ImportPath, Imports: p.Imports})
	}
	return out
}

// sourceLine is the header line protoc plugins write naming the .proto a
// file is generated from: "// source: acme/shop/orders/v1/orders.proto"
// (protoc-gen-go, protoc-gen-dedo-mcp) or "// Source: ..." (connect-go).
var sourceLine = regexp.MustCompile(`^// [Ss]ource: (\S+\.proto)$`)

// generatedPackages maps each package generated from the repository's
// protos to the module of their package ("" for a shared one), by the
// source line in its files' headers, wherever buf.gen.yaml writes them. It
// also returns the main packages.
func generatedPackages(pkgs []listedPackage, protos []protoFile) (map[string]string, map[string]bool) {
	generated, mains := map[string]string{}, map[string]bool{}
	for _, p := range pkgs {
		if p.Name == "main" {
			mains[p.ImportPath] = true
		}
		for _, f := range p.GoFiles {
			source := generatedFrom(filepath.Join(p.Dir, f))
			if source == "" {
				continue
			}
			if pkg, ok := protoPackageOf(source, protos); ok {
				_, module, _, versioned := protoModule(pkg)
				generated[p.ImportPath] = module
				if !versioned {
					generated[p.ImportPath] = ""
				}
			}
			break
		}
	}
	return generated, mains
}

// headerLines is how far into a file the source line may be.
const headerLines = 20

// generatedFrom returns the .proto a Go file says it is generated from, or
// "".
func generatedFrom(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < headerLines && sc.Scan(); i++ {
		if m := sourceLine.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
		if strings.HasPrefix(sc.Text(), "package ") {
			break
		}
	}
	return ""
}

// protoPackageOf returns the package of the repository's .proto at source,
// a path relative to its buf module.
func protoPackageOf(source string, protos []protoFile) (string, bool) {
	for _, p := range protos {
		if p.rel == source || strings.HasSuffix(p.rel, "/"+source) {
			return p.pkg, p.pkg != ""
		}
	}
	return "", false
}

// walkSkip are directories walkRepo never enters: vendored, generated for
// other languages' tooling, or test fixtures.
var walkSkip = map[string]bool{"node_modules": true, "vendor": true, "testdata": true, "third_party": true}

// walkRepo returns the repository's directories and the SQL files modules
// and shared code own, leaving out hidden directories and other Go modules
// nested inside it.
func walkRepo(dir string, l Layout) (dirs []string, sql []SQLFile, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || walkSkip[d.Name()] {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			dirs = append(dirs, rel)
			return nil
		}
		if !strings.HasSuffix(rel, ".sql") {
			return nil
		}
		kind, module := l.Classify(l.Module + "/" + filepath.ToSlash(filepath.Dir(rel)))
		if kind != KindModule && kind != KindShared {
			return nil
		}
		text, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sql = append(sql, SQLFile{Name: rel, Module: module, Text: string(text)})
		return nil
	})
	return dirs, sql, err
}

// packageSQL returns the SQL in the Go string literals of the modules' and
// shared packages, leaving out generated files, such as sqlc's, whose
// queries are in the .sql files already.
func packageSQL(dir string, l Layout, pkgs []listedPackage) ([]SQLFile, error) {
	fset := token.NewFileSet()
	var out []SQLFile
	for _, p := range pkgs {
		kind, module := l.Classify(p.ImportPath)
		if kind != KindModule && kind != KindShared {
			continue
		}
		for _, name := range p.GoFiles {
			path := filepath.Join(p.Dir, name)
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				return nil, err
			}
			if ast.IsGenerated(f) {
				continue
			}
			rel := path
			if real, err := realPath(path); err == nil {
				if r, err := filepath.Rel(dir, real); err == nil {
					rel = r
				}
			}
			out = append(out, goSQL(fset, filepath.ToSlash(rel), module, f)...)
		}
	}
	return out, nil
}

// realPath is path made absolute with its symbolic links resolved, so paths
// go list reports compare with it.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
