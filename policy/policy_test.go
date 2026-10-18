package policy_test

import (
	"strings"
	"testing"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
	"github.com/raveesh-me/http-proto-lint/internal/testsvc"
	"github.com/raveesh-me/http-proto-lint/policy"
)

func TestBuildFrom(t *testing.T) {
	files, err := testsvc.Files()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.Service)
	if err != nil {
		t.Fatal(err)
	}
	if len(table) != 2 {
		t.Fatalf("got %d methods, want 2", len(table))
	}
	m, ok := table[testsvc.GetThing]
	if !ok {
		t.Fatalf("missing %s", testsvc.GetThing)
	}
	if m.Procedure != testsvc.GetThing {
		t.Errorf("procedure = %q", m.Procedure)
	}
	if m.Audience != hplv1.Audience_AUDIENCE_EXTERNAL_PUBLIC {
		t.Errorf("audience = %v", m.Audience)
	}
	if got := m.Rules.GetScopes(); len(got) != 1 || got[0] != testsvc.ReadScope {
		t.Errorf("scopes = %v", got)
	}
}

func TestBuildFromFailures(t *testing.T) {
	tests := []struct {
		name    string
		edits   []testsvc.Edit
		service string
		want    string
	}{
		{name: "no services", want: "no services named"},
		{name: "unknown service", service: "raveesh_me.test.v1.Missing", want: "raveesh_me.test.v1.Missing"},
		{name: "not a service", service: "google.protobuf.Empty", want: "is not a service"},
		{name: "no service rules", edits: []testsvc.Edit{testsvc.WithoutServiceRules}, service: testsvc.Service, want: "no raveesh_me.http_proto_lint.v1.service option"},
		{name: "no scopes", edits: []testsvc.Edit{testsvc.WithoutScopes}, service: testsvc.Service, want: "DeleteThing declares no scopes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := testsvc.Files(tt.edits...)
			if err != nil {
				t.Fatal(err)
			}
			var services []string
			if tt.service != "" {
				services = []string{tt.service}
			}
			_, err = policy.BuildFrom(files, services...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestBuildUsesGlobalRegistry(t *testing.T) {
	if _, err := policy.Build("raveesh_me.test.v1.NotRegistered"); err == nil {
		t.Fatal("want error for unregistered service")
	}
}
