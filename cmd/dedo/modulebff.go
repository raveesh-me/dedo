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

// dataStoreClient is a family of data-store client libraries a BFF never
// depends on: the import path of a package or module, and every package
// below it, and the store it reaches.
type dataStoreClient struct {
	Prefix, Store string
}

// dataStoreClients are every data-store client a BFF never depends on, in
// one table: SQL and its drivers and ORMs, then document, key-value,
// wide-column, search and graph stores, and the cloud databases. A prefix
// without a major version, such as github.com/go-redis/redis, covers every
// one (/v7, /v8). database/sql/driver is not one: it holds only the
// interfaces that value types, such as UUIDs and decimals, implement to be
// stored, and opens nothing. guidance/install.md lists the families.
var dataStoreClients = []dataStoreClient{
	// SQL: the standard library, drivers, ORMs and query builders.
	{"database/sql", "SQL"},
	{"github.com/jackc/pgx", "PostgreSQL"},
	{"github.com/jackc/pgconn", "PostgreSQL"},
	{"github.com/lib/pq", "PostgreSQL"},
	{"github.com/go-pg/pg", "PostgreSQL"},
	{"github.com/go-sql-driver/mysql", "MySQL"},
	{"github.com/mattn/go-sqlite3", "SQLite"},
	{"modernc.org/sqlite", "SQLite"},
	{"github.com/microsoft/go-mssqldb", "SQL Server"},
	{"github.com/denisenkom/go-mssqldb", "SQL Server"},
	{"github.com/ClickHouse/clickhouse-go", "ClickHouse"},
	{"gorm.io", "SQL"},
	{"github.com/jinzhu/gorm", "SQL"},
	{"github.com/jmoiron/sqlx", "SQL"},
	{"github.com/uptrace/bun", "SQL"},
	{"entgo.io/ent", "SQL"},
	{"xorm.io", "SQL"},
	{"cloud.google.com/go/cloudsqlconn", "Cloud SQL"},
	{"cloud.google.com/go/alloydbconn", "AlloyDB"},
	// Document stores.
	{"go.mongodb.org/mongo-driver", "MongoDB"},
	{"gopkg.in/mgo.v2", "MongoDB"},
	{"github.com/globalsign/mgo", "MongoDB"},
	{"github.com/couchbase/gocb", "Couchbase"},
	{"cloud.google.com/go/firestore", "Firestore"},
	{"cloud.google.com/go/datastore", "Datastore"},
	// Key-value stores and caches.
	{"github.com/redis/go-redis", "Redis"},
	{"github.com/go-redis/redis", "Redis"},
	{"gopkg.in/redis.v5", "Redis"},
	{"github.com/gomodule/redigo", "Redis"},
	{"github.com/garyburd/redigo", "Redis"},
	{"github.com/redis/rueidis", "Redis"},
	{"github.com/valkey-io/valkey-go", "Valkey"},
	{"github.com/bradfitz/gomemcache", "Memcached"},
	{"github.com/aws/aws-sdk-go-v2/service/dynamodb", "DynamoDB"},
	{"github.com/aws/aws-sdk-go-v2/feature/dynamodb", "DynamoDB"},
	{"github.com/aws/aws-sdk-go/service/dynamodb", "DynamoDB"},
	{"github.com/guregu/dynamo", "DynamoDB"},
	// Wide-column stores and warehouses.
	{"github.com/gocql/gocql", "Cassandra"},
	{"github.com/apache/cassandra-gocql-driver", "Cassandra"},
	{"github.com/scylladb/gocqlx", "Cassandra"},
	{"cloud.google.com/go/bigtable", "Bigtable"},
	{"cloud.google.com/go/spanner", "Spanner"},
	{"cloud.google.com/go/bigquery", "BigQuery"},
	// Search and graph stores.
	{"github.com/elastic/go-elasticsearch", "Elasticsearch"},
	{"github.com/olivere/elastic", "Elasticsearch"},
	{"github.com/opensearch-project/opensearch-go", "OpenSearch"},
	{"github.com/neo4j/neo4j-go-driver", "Neo4j"},
}

// isDatabaseDriver reports whether the package at importPath is a
// data-store client, one of dataStoreClients.
func isDatabaseDriver(importPath string) bool {
	if importPath == "database/sql/driver" {
		return false
	}
	for _, c := range dataStoreClients {
		if inPath(importPath, c.Prefix) {
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
// packages that depends on a database driver, and each that depends on the
// backend's own code, which is everything of the backend's Go module but
// the packages generated from its protos.
//
// Both count when any package the BFF imports reaches them, not only when
// the BFF imports them itself. A driver in the BFF's binary is either
// unused or a way around the rule, such as a library or SDK that opens a
// database for it, and a BFF holds no database credentials to use one. The
// backend's code is reachable through another Go module nested under the
// backend's path, which Go lets import the backend's internal/.
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
			if chain := b.chain(imp, byPath, isDatabaseDriver); chain != nil {
				out = append(out, fmt.Sprintf("%s: depends on the database driver %s%s: a backend for frontend owns no data; read it through the backend's Connect API", rel, chain[len(chain)-1], through(chain)))
			}
			backend := func(p string) bool {
				q, ok := byPath[p]
				_, generated := l.Generated[p]
				return ok && q.Module != nil && q.Module.Path == l.Module && !generated
			}
			switch chain := b.chain(imp, byPath, backend); {
			case len(chain) == 1:
				out = append(out, fmt.Sprintf("%s: imports %s, the backend's own code: read the backend only through its Connect API, with the clients generated from its protos", rel, l.rel(imp)))
			case chain != nil:
				out = append(out, fmt.Sprintf("%s: depends on %s, the backend's own code,%s: read the backend only through its Connect API, with the clients generated from its protos", rel, l.rel(chain[len(chain)-1]), through(chain)))
			}
		}
	}
	return out, nil
}

// chain returns the shortest chain of imports from the package at start to
// one that match reports, start first and that package last, through
// packages outside the BFF, or nil when there is none.
func (b bff) chain(start string, byPath map[string]listedPackage, match func(string) bool) []string {
	from := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if match(p) {
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

// through names the packages of a chain between its start and its end:
// " through a -> b", or "" when the start is the end.
func through(chain []string) string {
	if len(chain) < 2 {
		return ""
	}
	return " through " + strings.Join(chain[:len(chain)-1], " -> ")
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
