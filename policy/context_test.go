package policy_test

import (
	"testing"

	"github.com/raveesh-me/dedo/policy"
)

func TestMethodContext(t *testing.T) {
	if _, ok := policy.MethodFrom(t.Context()); ok {
		t.Fatal("a bare context carries a method")
	}
	want := policy.Method{Procedure: "/raveesh_me.test.v1.TestService/DeleteThing"}
	got, ok := policy.MethodFrom(policy.WithMethod(t.Context(), want))
	if !ok || got.Procedure != want.Procedure {
		t.Fatalf("MethodFrom = %q, %v; want %q, true", got.Procedure, ok, want.Procedure)
	}
}
