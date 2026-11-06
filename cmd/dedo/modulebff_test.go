package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIsDatabaseDriver(t *testing.T) {
	for p, want := range map[string]bool{
		"database/sql":                           true,
		"database/sql/driver":                    false,
		"github.com/jackc/pgx":                   true,
		"github.com/jackc/pgx/v5":                true,
		"github.com/jackc/pgx/v5/pgxpool":        true,
		"github.com/jackc/pgxlisten":             false,
		"github.com/lib/pq":                      true,
		"gorm.io/gorm":                           true,
		"gorm.io/driver/postgres":                true,
		"go.mongodb.org/mongo-driver/v2/mongo":   true,
		"github.com/redis/go-redis/v9":           true,
		"modernc.org/sqlite":                     true,
		"github.com/google/uuid":                 false,
		"connectrpc.com/connect":                 false,
		"github.com/go-sql-driver/mysql":         true,
		"github.com/mattn/go-sqlite3":            true,
		"example.com/shop/internal/shared/db":    false,
		"example.com/shop/gen/go/shop/orders/v1": false,
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
