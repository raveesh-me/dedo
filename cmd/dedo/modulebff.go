package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// A backend for frontend (BFF) serves exactly one frontend, built from the
// same commit. In a service repository it is a Go module nested inside it,
// a directory with its own go.mod, such as bff/, that holds one of
// buf.yaml's modules, such as bff/proto. That module has no name, so buf
// push --exclude-unnamed never publishes it, and its services are
// AUDIENCE_COMPONENT_INTERNAL. The BFF owns no data and reads the backend
// only through its Connect API; the backend never imports the BFF
// (guidance/domains.md).
type bff struct {
	// Dir is the BFF's directory, relative and slash-separated: bff.
	Dir string
	// Module is its Go module path: example.com/shop/bff.
	Module string
}

// bffAudience is the audience of every service of a BFF, and of no
// published module's.
const bffAudience = "AUDIENCE_COMPONENT_INTERNAL"

// databaseDrivers are the database drivers a BFF never depends on: each
// import path and every package below it. database/sql/driver is not one:
// it holds only the interfaces that value types, such as UUIDs and
// decimals, implement to be stored, and opens nothing.
var databaseDrivers = []string{
	"database/sql",
	"github.com/jackc/pgx",
	"github.com/lib/pq",
	"github.com/go-sql-driver/mysql",
	"gorm.io",
	"github.com/mattn/go-sqlite3",
	"modernc.org/sqlite",
	"go.mongodb.org/mongo-driver",
	"github.com/redis/go-redis",
}

// isDatabaseDriver reports whether the package at importPath is one of the
// databaseDrivers.
func isDatabaseDriver(importPath string) bool {
	if importPath == "database/sql/driver" {
		return false
	}
	for _, d := range databaseDrivers {
		if inPath(importPath, d) {
			return true
		}
	}
	return false
}

// inPath reports whether importPath is prefix or a package below it.
func inPath(importPath, prefix string) bool {
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}

var (
	audienceLit = regexp.MustCompile(`audience\s*[:=]\s*(AUDIENCE_[A-Z_]+)`)
	commentLine = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// checkBFFs finds the repository's BFFs, the nested Go modules that hold a
// buf.yaml module, and checks them: their buf modules are unnamed and their
// services AUDIENCE_COMPONENT_INTERNAL, which no published module's are;
// they depend on no database driver and on none of the backend's own code
// but its generated clients; and the backend, whose packages are pkgs,
// never imports them.
func checkBFFs(dir string, l Layout, pkgs []listedPackage) ([]bff, []string, error) {
	mods, err := bufModules(dir)
	if err != nil {
		return nil, nil, err
	}
	nested := nestedGoModules(dir)
	var bffs []bff
	var findings []string
	for _, m := range mods {
		d, inside := insideModule(m.Path, nested)
		switch {
		case inside:
			b, err := bffAt(dir, d, bffs)
			if err != nil {
				return nil, nil, err
			}
			if b != nil {
				bffs = append(bffs, *b)
			}
			if m.Name != "" {
				findings = append(findings, fmt.Sprintf("%s: buf.yaml names the module %s, which serves the frontend of the Go module in %s: remove the name, so buf push --exclude-unnamed never publishes it", m.Path, m.Name, d))
			}
			findings = append(findings, audiences(dir, m.Path, func(file, svc, audience string) string {
				if audience == bffAudience {
					return ""
				}
				if audience == "" {
					audience = "declares no audience"
				} else {
					audience = "is " + audience
				}
				return fmt.Sprintf("%s: service %s %s, but serves the frontend of the Go module in %s: declare audience: %s", file, svc, audience, d, bffAudience)
			})...)
		case m.Name != "":
			findings = append(findings, audiences(dir, m.Path, func(file, svc, audience string) string {
				if audience != bffAudience {
					return ""
				}
				return fmt.Sprintf("%s: service %s is %s in %s, a published module: only a backend for frontend's services are; declare the audience of its callers", file, svc, bffAudience, m.Name)
			})...)
		}
	}
	for _, b := range bffs {
		found, err := b.check(dir, l)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, found...)
	}
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			for _, b := range bffs {
				if inPath(imp, b.Module) {
					findings = append(findings, fmt.Sprintf("%s: imports %s, the backend for frontend in %s: the backend never imports its frontend's BFF; move what they share into the backend", l.rel(p.ImportPath), imp, b.Dir))
				}
			}
		}
	}
	return bffs, findings, nil
}

// bffAt returns the BFF in the nested Go module at rel, or nil when bffs
// has it already.
func bffAt(dir, rel string, bffs []bff) (*bff, error) {
	for _, b := range bffs {
		if b.Dir == rel {
			return nil, nil
		}
	}
	gomod := filepath.Join(dir, filepath.FromSlash(rel), "go.mod")
	src, err := os.ReadFile(gomod)
	if err != nil {
		return nil, err
	}
	m := moduleLine.FindSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("%s: no module line", gomod)
	}
	return &bff{Dir: rel, Module: string(m[1])}, nil
}

// audiences calls finding with the audience of every service in the buf
// module at mod, "" when a service declares none, and returns the findings
// it makes.
func audiences(dir, mod string, finding func(file, svc, audience string) string) []string {
	rels, err := findProtos(filepath.Join(dir, filepath.FromSlash(mod)))
	if err != nil {
		return nil
	}
	var out []string
	for _, rel := range rels {
		file := path.Join(mod, filepath.ToSlash(rel))
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		for _, s := range serviceAudiences(string(src)) {
			if f := finding(file, s[0], s[1]); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// serviceAudiences returns each service of a .proto source with the
// audience its dedo.v1.service option declares, or "".
func serviceAudiences(src string) [][2]string {
	src = commentLine.ReplaceAllString(src, "")
	var out [][2]string
	for _, m := range protoSvc.FindAllStringSubmatchIndex(src, -1) {
		body := src[m[1]:]
		depth := 1
		for i, r := range body {
			switch r {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				body = body[:i]
				break
			}
		}
		audience := ""
		if a := audienceLit.FindStringSubmatch(body); a != nil {
			audience = a[1]
		}
		out = append(out, [2]string{src[m[2]:m[3]], audience})
	}
	return out
}

// check lists the BFF's packages and returns its findings: each of its
// packages that depends on a database driver, and each that imports the
// backend's own code, which is everything of the backend's Go module but
// the packages generated from its protos.
//
// A driver counts when any package the BFF imports reaches it, not only
// when the BFF imports it itself: a driver in the BFF's binary is either
// unused or a way around the rule, such as a library or SDK that opens a
// database for it, and a BFF holds no database credentials to use one.
// Value types that store themselves import database/sql/driver only, which
// is not a driver, so a UUID or decimal library does not count. Each
// finding names the BFF package where the dependency enters, and the
// imports from it to the driver.
func (b bff) check(dir string, l Layout) ([]string, error) {
	all, err := listDeps(filepath.Join(dir, filepath.FromSlash(b.Dir)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Dir, err)
	}
	byPath := map[string]listedPackage{}
	for _, p := range all {
		byPath[p.ImportPath] = p
	}
	var out []string
	for _, p := range ownPackages(all, b.Module) {
		rel := relDir(dir, p.Dir)
		for _, imp := range p.Imports {
			if inPath(imp, b.Module) {
				continue
			}
			if chain := b.driverChain(imp, byPath); chain != nil {
				via := ""
				if len(chain) > 1 {
					via = " through " + strings.Join(chain[:len(chain)-1], " -> ")
				}
				out = append(out, fmt.Sprintf("%s: depends on the database driver %s%s: a backend for frontend owns no data; read it through the backend's Connect API", rel, chain[len(chain)-1], via))
			}
			if q, ok := byPath[imp]; ok && q.Module != nil && q.Module.Path == l.Module {
				if _, generated := l.Generated[imp]; !generated {
					out = append(out, fmt.Sprintf("%s: imports %s, the backend's own code: read the backend only through its Connect API, with the clients generated from its protos", rel, l.rel(imp)))
				}
			}
		}
	}
	return out, nil
}

// driverChain returns the shortest chain of imports from the package at
// start to a database driver, start first and the driver last, through
// packages outside the BFF, or nil when there is none.
func (b bff) driverChain(start string, byPath map[string]listedPackage) []string {
	from := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if isDatabaseDriver(p) {
			var chain []string
			for q := p; q != ""; q = from[q] {
				chain = append([]string{q}, chain...)
			}
			return chain
		}
		for _, imp := range byPath[p].Imports {
			if _, seen := from[imp]; seen || inPath(imp, b.Module) {
				continue
			}
			from[imp] = p
			queue = append(queue, imp)
		}
	}
	return nil
}

// relDir is path relative to the repository at dir, slash-separated, or
// path itself when it is outside it.
func relDir(dir, p string) string {
	if real, err := realPath(p); err == nil {
		if r, err := filepath.Rel(dir, real); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return p
}
