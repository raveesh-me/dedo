package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// Every audience of the options module is a BFF's or not, by name: its
// frontend's only, or external; never another internal audience, unset or
// a name the module doesn't have.
func TestIsBFFAudience(t *testing.T) {
	want := map[string]bool{
		"AUDIENCE_UNSPECIFIED":            false,
		"AUDIENCE_COMPONENT_INTERNAL":     true,
		"AUDIENCE_BUSINESS_UNIT_INTERNAL": false,
		"AUDIENCE_COMPANY_INTERNAL":       false,
		"AUDIENCE_EXTERNAL_PARTNER":       true,
		"AUDIENCE_EXTERNAL_PUBLIC":        true,
	}
	for _, name := range dedov1.Audience_name {
		if _, ok := want[name]; !ok {
			t.Errorf("audience %s: say whether a BFF may declare it", name)
		}
	}
	for audience, ok := range want {
		if got := isBFFAudience(audience); got != ok {
			t.Errorf("isBFFAudience(%q) = %v, want %v", audience, got, ok)
		}
	}
	for _, audience := range []string{"", "AUDIENCE_EXTERNAL", "EXTERNAL_PUBLIC", "audience_external_public"} {
		if isBFFAudience(audience) {
			t.Errorf("isBFFAudience(%q) = true, want false", audience)
		}
	}
	for _, a := range bffAudiences() {
		if _, ok := dedov1.Audience_value[a]; !ok {
			t.Errorf("bffAudiences() has %s, which is no audience of the options module", a)
		}
	}
	if bffAudiences()[0] != bffAudience {
		t.Errorf("bffAudiences() = %v: dedo init -bff's audience, %s, comes first", bffAudiences(), bffAudience)
	}
}

func TestIsDatabaseDriver(t *testing.T) {
	for p, want := range map[string]bool{
		"database/sql":                         true,
		"database/sql/driver":                  false,
		"github.com/jackc/pgx":                 true,
		"github.com/jackc/pgx/v5":              true,
		"github.com/jackc/pgx/v5/pgxpool":      true,
		"github.com/jackc/pgxlisten":           false,
		"github.com/lib/pq":                    true,
		"gorm.io/gorm":                         true,
		"gorm.io/driver/postgres":              true,
		"go.mongodb.org/mongo-driver/v2/mongo": true,
		"github.com/redis/go-redis/v9":         true,
		"modernc.org/sqlite":                   true,
		"github.com/google/uuid":               false,
		"connectrpc.com/connect":               false,
		"github.com/go-sql-driver/mysql":       true,
		"github.com/mattn/go-sqlite3":          true,
		"example.com/shop/internal/shared/db":  false,
		// Every data-store family, by its module's import path, older
		// major versions included.
		"github.com/go-redis/redis":                                    true,
		"github.com/go-redis/redis/v7":                                 true,
		"github.com/go-redis/redis/v8":                                 true,
		"github.com/redis/go-redis/v9/internal/pool":                   true,
		"github.com/gomodule/redigo/redis":                             true,
		"github.com/garyburd/redigo/redis":                             true,
		"github.com/redis/rueidis":                                     true,
		"cloud.google.com/go/firestore":                                true,
		"cloud.google.com/go/firestore/apiv1":                          true,
		"cloud.google.com/go/spanner":                                  true,
		"cloud.google.com/go/bigtable":                                 true,
		"cloud.google.com/go/bigquery":                                 true,
		"cloud.google.com/go/bigquery/storage/apiv1":                   true,
		"cloud.google.com/go/datastore":                                true,
		"cloud.google.com/go/cloudsqlconn":                             true,
		"github.com/aws/aws-sdk-go-v2/service/dynamodb":                true,
		"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue": true,
		"github.com/aws/aws-sdk-go/service/dynamodb":                   true,
		"github.com/elastic/go-elasticsearch/v8":                       true,
		"github.com/olivere/elastic/v7":                                true,
		"github.com/opensearch-project/opensearch-go/v4":               true,
		"github.com/gocql/gocql":                                       true,
		"github.com/apache/cassandra-gocql-driver/v2":                  true,
		"go.mongodb.org/mongo-driver/v2/bson":                          true,
		"github.com/jmoiron/sqlx":                                      true,
		"github.com/uptrace/bun":                                       true,
		"entgo.io/ent":                                                 true,
		"github.com/ClickHouse/clickhouse-go/v2":                       true,
		"github.com/microsoft/go-mssqldb":                              true,
		"github.com/couchbase/gocb/v2":                                 true,
		"github.com/neo4j/neo4j-go-driver/v5/neo4j":                    true,
		"github.com/bradfitz/gomemcache/memcache":                      true,
		// Neighbours that store nothing, or only the prefix's name.
		"cloud.google.com/go/pubsub":               false,
		"cloud.google.com/go/compute/metadata":     false,
		"cloud.google.com/go/firestorex":           false,
		"github.com/aws/aws-sdk-go-v2/service/sqs": false,
		"github.com/go-redis/redismock":            false,
		"github.com/elastic/go-sysinfo":            false,
		"example.com/shop/gen/go/shop/orders/v1":   false,
	} {
		if got := isDatabaseDriver(p); got != want {
			t.Errorf("isDatabaseDriver(%s) = %v", p, got)
		}
	}
}

func TestServiceAudiences(t *testing.T) {
	src := `syntax = "proto3";
// service CommentedService {
service WebService {
  // audience: AUDIENCE_EXTERNAL_PUBLIC is not what it declares.
  option (dedo.v1.service) = {
    tenancy: {collection: "locations"}
    audience: AUDIENCE_COMPONENT_INTERNAL
  };
  rpc Get(GetRequest) returns (GetResponse) {
    option (google.api.http) = {get: "/v1/{name=webs/*}"};
  }
}
message GetRequest { string audience = 1; }
service PlainService {}
`
	got := serviceAudiences(src)
	want := [][2]string{{"WebService", "AUDIENCE_COMPONENT_INTERNAL"}, {"PlainService", ""}}
	if !slices.Equal(got, want) {
		t.Errorf("serviceAudiences = %v, want %v", got, want)
	}
}

// dedo init and dedo upgrade leave a backend for frontend's protos alone:
// its services get no handler in internal/ and no place in main.go, and
// buf.yaml's first module outside it is the proto root.
func TestInitLeavesOutBFF(t *testing.T) {
	proto := func(pkg, svc string) string {
		return "syntax = \"proto3\";\npackage " + pkg + ";\nservice " + svc + " {}\n"
	}
	dir := repo(t, map[string]string{
		"go.mod":                           "module example.com/shop\n",
		"buf.yaml":                         "version: v2\nmodules:\n  - path: bff/proto\n  - path: proto\n",
		"proto/shop/order/v1/order.proto":  proto("shop.order.v1", "OrderService"),
		"bff/go.mod":                       "module example.com/shop/bff\n",
		"bff/proto/shop/web/v1/web.proto":  proto("shop.web.v1", "WebService"),
		"bff/nested/proto/x/v1/x.proto":    proto("x.v1", "XService"),
		"tools/go.mod":                     "module example.com/shop/tools\n",
		"tools/proto/shop/tool/v1/t.proto": proto("shop.tool.v1", "ToolService"),
	})
	p, err := analyze(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range p.services {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"OrderService"}) || p.protos != 1 || p.protoRoot != "proto" {
		t.Fatalf("services %v, %d protos, root %q; want OrderService, 1, proto", names, p.protos, p.protoRoot)
	}
	initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "internal", "web")); err == nil {
		t.Error("dedo init wrote a handler for the BFF's WebService in internal/web")
	}
	if main := read(t, dir, "main.go"); strings.Contains(main, "WebService") || !strings.Contains(main, "OrderService") {
		t.Errorf("main.go:\n%s", main)
	}
}

// Every entry of the table names its store and is no prefix of another,
// which would make it unreachable.
func TestDataStoreClientsTable(t *testing.T) {
	for i, c := range dataStoreClients {
		if c.Prefix == "" || c.Store == "" {
			t.Errorf("entry %d: %+v", i, c)
		}
		for j, d := range dataStoreClients {
			if i != j && inPath(d.Prefix, c.Prefix) {
				t.Errorf("%s is below %s already", d.Prefix, c.Prefix)
			}
		}
	}
}
