package rules

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestModuleName(t *testing.T) {
	for pkg, want := range map[protoreflect.FullName]string{
		"acme.orders.v1":          "orders",
		"acme.shared.v1":          "shared",
		"acme.shared.v2beta1":     "shared",
		"acme.shared":             "shared",
		"shared.v1":               "shared",
		"acme.type":               "type",
		"acme.shared.orders.v1":   "orders",
		"raveesh_me.todo.v1alpha": "todo",
		"dedo.v1":                 "dedo",
		"v1":                      "v1",
		"":                        "",
	} {
		if got := moduleName(pkg); got != want {
			t.Errorf("moduleName(%q) = %q, want %q", pkg, got, want)
		}
	}
}
