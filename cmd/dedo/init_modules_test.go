package main

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each module gets its handlers in internal/<module>/, every version of a
// module in the same package, and main.go registers every service, under
// an alias where a module's name is one main.go uses.
func TestInitScaffoldsModules(t *testing.T) {
	proto := func(pkg, svc string) string {
		return "syntax = \"proto3\";\npackage " + pkg + ";\nservice " + svc + " {}\n"
	}
	dir := repo(t, map[string]string{
		"go.mod":                          "module example.com/shop\n",
		"proto/shop/cart/v1/cart.proto":   proto("shop.cart.v1", "CartService"),
		"proto/shop/cart/v2/cart.proto":   proto("shop.cart.v2", "CartService"),
		"proto/shop/order/v1/order.proto": proto("shop.order.v1", "OrderService"),
		"proto/shop/docs/v1/docs.proto":   proto("shop.docs.v1", "DocsService"),
	})
	out := initOffline(t, dir)
	stubs := map[string][]string{
		"internal/cart/cart_service.go":    {"package cart\n", "type CartService struct", "cartv1connect.UnimplementedCartServiceHandler"},
		"internal/cart/cart_service_v2.go": {"package cart\n", "type CartServiceV2 struct", "cartv2connect.UnimplementedCartServiceHandler", "func NewCartServiceV2() *CartServiceV2"},
		"internal/order/order_service.go":  {"package order\n", "func NewOrderService() *OrderService"},
		"internal/docs/docs_service.go":    {"package docs\n"},
	}
	for name, wants := range stubs {
		got := read(t, dir, name)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s lacks %q:\n%s", name, want, got)
			}
		}
	}
	main := read(t, dir, "main.go")
	if formatted, err := format.Source([]byte(main)); err != nil || string(formatted) != main {
		t.Fatalf("main.go is not gofmt-formatted (%v):\n%s", err, main)
	}
	for _, want := range []string{
		`"example.com/shop/internal/cart"`,
		`docsmodule "example.com/shop/internal/docs"`,
		"cartv1connect.RegisterCartServiceHandler(server, cart.NewCartService())",
		"cartv2connect.RegisterCartServiceHandler(server, cart.NewCartServiceV2())",
		"docsv1connect.RegisterDocsServiceHandler(server, docsmodule.NewDocsService())",
		"orderv1connect.OrderServiceName,",
		"mcpserver.AddTools(tools, client, cartv2.CartServiceMCPTools()...)",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s:\n%s", want, main)
		}
	}
	if strings.Count(main, `"example.com/shop/internal/cart"`) != 1 {
		t.Errorf("main.go imports a module twice:\n%s", main)
	}
	if !strings.Contains(out, "main.go: created; it serves CartService, CartService, DocsService, OrderService") {
		t.Errorf("output:\n%s", out)
	}
	test := read(t, dir, "policy_table_test.go")
	for _, want := range []string{"cartv1connect.CartServiceName,", "cartv2connect.CartServiceName,", "orderv1connect.OrderServiceName,"} {
		if !strings.Contains(test, want) {
			t.Errorf("policy test lacks %s:\n%s", want, test)
		}
	}
	before := snapshot(t, dir)
	initOffline(t, dir)
	if !sameFiles(before, snapshot(t, dir)) {
		t.Error("a second dedo init changed files")
	}
}

// An upgrade keeps the handlers the repository owns: a handler moved to
// another file of its module is not stubbed again, and a service added
// later gets a stub, with a note that main.go does not register it yet.
func TestInitKeepsModuleHandlers(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                          "module example.com/shop\n",
		"proto/shop/cart/v1/cart.proto":   "syntax = \"proto3\";\npackage shop.cart.v1;\nservice CartService {}\n",
		"proto/shop/order/v1/order.proto": "syntax = \"proto3\";\npackage shop.order.v1;\nservice OrderService {}\n",
	})
	initOffline(t, dir)
	stub := filepath.Join(dir, "internal/cart/cart_service.go")
	src, err := os.ReadFile(stub)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "internal/cart/handlers.go", string(src)+"\n// mine\n")
	if err := os.Remove(stub); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "proto/shop/billing/v1/billing.proto", "syntax = \"proto3\";\npackage shop.billing.v1;\nservice BillingService {}\n")

	out := initOffline(t, dir)
	if _, err := os.Stat(stub); err == nil {
		t.Error("a handler moved within its module was stubbed again")
	}
	if !strings.Contains(out, "internal/cart/cart_service.go: internal/cart/handlers.go declares CartService already; left as it is") {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/billing/billing_service.go")); err != nil {
		t.Errorf("no stub for the new service: %v", err)
	}
	if !strings.Contains(out, "main.go registers no handler for BillingService: dedo's template below registers every service") {
		t.Errorf("output does not say main.go misses the new service:\n%s", out)
	}
	if strings.Contains(read(t, dir, "main.go"), "BillingService") {
		t.Error("the user's main.go was changed")
	}
}
