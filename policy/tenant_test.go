package policy_test

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/policy"
)

func TestTenantFields(t *testing.T) {
	files, err := testsvc.TenantFiles()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.TenantService)
	if err != nil {
		t.Fatal(err)
	}
	for procedure, want := range map[string]string{
		testsvc.GetTenantThing:    "name",
		testsvc.UpdateTenantThing: "thing.name",
	} {
		if got := table[procedure].Tenant; got == nil || got.Field != want || got.Collection != "locations" {
			t.Errorf("%s tenant = %+v, want field %q in locations", procedure, got, want)
		}
	}
	if got := table[testsvc.ListTenants].Tenant; got != nil {
		t.Errorf("cross-tenant ListTenants has tenant %+v", got)
	}
}

// A tenant-scoped method whose tenant cannot be found must stop the service.
func TestTenantFieldMissingFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		edit testsvc.TenantEdit
		want string
	}{
		{"no HTTP binding", testsvc.WithoutHTTP, "has no HTTP binding"},
		{"no tenant in the path", testsvc.WithHTTPGet("/v1/{name=things/*}"), "binds no field to locations/*"},
		{"bare variable", testsvc.WithHTTPGet("/v1/{name}"), "binds no field to locations/*"},
		{"bindings disagree", testsvc.WithAdditionalGet("/v1/{other=locations/*/things/*}"), `name the tenant in "name" and "other"`},
		{"unknown field", testsvc.WithHTTPGet("/v1/{missing=locations/*/things/*}"), `no singular field "missing"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := testsvc.TenantFiles(tt.edit)
			if err != nil {
				t.Fatal(err)
			}
			_, err = policy.BuildFrom(files, testsvc.TenantService)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTenantOf(t *testing.T) {
	files, err := testsvc.TenantFiles()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.TenantService)
	if err != nil {
		t.Fatal(err)
	}
	get := newRequest(t, files, "raveesh_me.tenant.v1.GetThingRequest")
	nameField := get.Descriptor().Fields().ByName("name")
	tests := []struct {
		name, value, want string
		ok                bool
	}{
		{"resource", "locations/1/things/2", "1", true},
		{"tenant itself", "locations/1", "1", true},
		{"across tenants", "locations/-/things/2", "-", true},
		{"other collection", "things/2", "", false},
		{"empty id", "locations//things/2", "", false},
		{"empty", "", "", false},
		{"prefix only", "locationsx/1", "", false},
		// The whole name must be well formed, so a handler that cleans it
		// cannot land in another tenant.
		{"dot-dot", "locations/1/../2/things/3", "", false},
		{"dot", "locations/1/./things/3", "", false},
		{"trailing slash", "locations/1/things/", "", false},
		{"space", "locations/1/things/ 2", "", false},
		{"newline", "locations/1\n/things/2", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			get.Set(nameField, protoreflect.ValueOfString(tt.value))
			got, ok := table[testsvc.GetTenantThing].Tenant.Of(get)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Of(%q) = %q, %v; want %q, %v", tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}

	update := newRequest(t, files, "raveesh_me.tenant.v1.UpdateThingRequest")
	if _, ok := table[testsvc.UpdateTenantThing].Tenant.Of(update); ok {
		t.Error("an unset nested field names no tenant")
	}
	thingField := update.Descriptor().Fields().ByName("thing")
	thing := update.Mutable(thingField).Message()
	thing.Set(thing.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("locations/7/things/1"))
	if got, ok := table[testsvc.UpdateTenantThing].Tenant.Of(update); got != "7" || !ok {
		t.Errorf("nested Of = %q, %v; want 7", got, ok)
	}
}

// Every resource reference in a request that names a tenant is a tenant the
// caller must be allowed in, so a move cannot reach into another tenant.
func TestTenantsOfReferences(t *testing.T) {
	files, err := testsvc.TenantFiles()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.TenantService)
	if err != nil {
		t.Fatal(err)
	}
	tenant := table[testsvc.MoveThing].Tenant
	tests := []struct {
		name    string
		set     func(*dynamicpb.Message)
		want    []string
		wantBad bool
	}{
		{"tenant field only", func(*dynamicpb.Message) {}, []string{"1"}, false},
		{"reference in the same tenant", func(m *dynamicpb.Message) { setString(m, "destination", "locations/1/things/9") }, []string{"1"}, false},
		{"reference in another tenant", func(m *dynamicpb.Message) { setString(m, "destination", "locations/2") }, []string{"1", "2"}, false},
		{"list of references", func(m *dynamicpb.Message) {
			list := m.Mutable(m.Descriptor().Fields().ByName("copies")).List()
			list.Append(protoreflect.ValueOfString("locations/1/things/8"))
			list.Append(protoreflect.ValueOfString("locations/3/things/8"))
		}, []string{"1", "3"}, false},
		{"nested reference", func(m *dynamicpb.Message) {
			target := m.Mutable(m.Descriptor().Fields().ByName("target")).Message()
			target.Set(target.Descriptor().Fields().ByName("location"), protoreflect.ValueOfString("locations/4"))
		}, []string{"1", "4"}, false},
		{"reference outside the tenants", func(m *dynamicpb.Message) { setString(m, "destination", "catalog/7") }, []string{"1"}, false},
		{"field that is not a reference", func(m *dynamicpb.Message) { setString(m, "note", "locations/5") }, []string{"1"}, false},
		{"malformed reference", func(m *dynamicpb.Message) { setString(m, "destination", "locations/1/../2") }, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, files, "raveesh_me.tenant.v1.MoveThingRequest")
			setString(req, "name", "locations/1/things/2")
			tt.set(req)
			got, ok := tenant.Tenants(req)
			if ok == tt.wantBad || !slices.Equal(got, tt.want) {
				t.Fatalf("Tenants = %v, %v; want %v, %v", got, ok, tt.want, !tt.wantBad)
			}
		})
	}
}

// A path that binds two fields to the tenant is ambiguous.
func TestTenantFieldTwiceFailsClosed(t *testing.T) {
	files, err := testsvc.TenantFiles(testsvc.WithHTTPGet("/v1/{name=locations/*/things/*}/{name2=locations/*}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.BuildFrom(files, testsvc.TenantService); err == nil || !strings.Contains(err.Error(), "more than one field") {
		t.Fatalf("err = %v", err)
	}
}

func setString(m *dynamicpb.Message, field, value string) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(field)), protoreflect.ValueOfString(value))
}

func TestTenantField(t *testing.T) {
	tests := []struct {
		path, field string
		ok          bool
	}{
		{"/commerce/todo/v1/{parent=locations/*}/todos", "parent", true},
		{"/commerce/todo/v1/{todo.name=locations/*/todos/*}", "todo.name", true},
		{"/commerce/todo/v1/{name=locations/**}", "", false},
		{"/commerce/todo/v1/{name=shops/*/todos/*}", "", false},
		{"/commerce/todo/v1/locations", "", false},
	}
	for _, tt := range tests {
		field, ok := policy.TenantField(tt.path, "locations")
		if field != tt.field || ok != tt.ok {
			t.Errorf("TenantField(%q) = %q, %v; want %q, %v", tt.path, field, ok, tt.field, tt.ok)
		}
	}
}

func newRequest(t *testing.T, files *protoregistry.Files, name string) *dynamicpb.Message {
	t.Helper()
	d, err := files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		t.Fatal(err)
	}
	return dynamicpb.NewMessage(d.(protoreflect.MessageDescriptor))
}
