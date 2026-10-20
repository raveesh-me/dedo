package interceptor_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/internal/testsvc"
	"github.com/raveesh-me/dedo/policy"
)

// tenantFixture serves the multi-tenant test service to a caller who may
// act in the tenants listed.
type tenantFixture struct {
	url     string
	files   *protoregistry.Files
	log     *bytes.Buffer
	handled *int
}

func newTenantFixture(t *testing.T, tenants ...string) tenantFixture {
	t.Helper()
	files, err := testsvc.TenantFiles()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.TenantService)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        func(context.Context) []string { return []string{testsvc.ReadScope} },
		EnforceScopes: true,
		Tenants:       func(_ context.Context, tenant string) bool { return slices.Contains(tenants, tenant) },
		Log:           slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	handled := new(int)
	server := connect.NewServer(guard)
	for procedure, in := range map[string]string{
		testsvc.GetTenantThing:    "GetThingRequest",
		testsvc.UpdateTenantThing: "UpdateThingRequest",
		testsvc.ListTenants:       "ListTenantsRequest",
	} {
		reqType := message(t, files, in)
		server.Register(connect.Method{Spec: unarySpec(procedure), Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			req := dynamicpb.NewMessage(reqType)
			if err := stream.Receive(req); err != nil {
				return err
			}
			*handled++
			return stream.Send(dynamicpb.NewMessage(reqType))
		}})
	}
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return tenantFixture{url: srv.URL, files: files, log: &log, handled: handled}
}

func unarySpec(procedure string) connect.Spec {
	return connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary}
}

func message(t *testing.T, files *protoregistry.Files, name string) protoreflect.MessageDescriptor {
	t.Helper()
	d, err := files.FindDescriptorByName(protoreflect.FullName("raveesh_me.tenant.v1." + name))
	if err != nil {
		t.Fatal(err)
	}
	return d.(protoreflect.MessageDescriptor)
}

// call sends req to procedure and returns the error.
func (f tenantFixture) call(t *testing.T, procedure string, req proto.Message) error {
	t.Helper()
	client := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, f.url))
	res := dynamicpb.NewMessage(req.ProtoReflect().Descriptor())
	return client.CallUnary(t.Context(), unarySpec(procedure), req, res)
}

// getThing returns a GetThing request for a resource name.
func (f tenantFixture) getThing(t *testing.T, name string) proto.Message {
	t.Helper()
	req := dynamicpb.NewMessage(message(t, f.files, "GetThingRequest"))
	req.Set(req.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString(name))
	return req
}

func TestNewRequiresTenantAccess(t *testing.T) {
	files, err := testsvc.TenantFiles()
	if err != nil {
		t.Fatal(err)
	}
	table, err := policy.BuildFrom(files, testsvc.TenantService)
	if err != nil {
		t.Fatal(err)
	}
	_, err = interceptor.New(interceptor.Config{Table: table, Scopes: func(context.Context) []string { return nil }})
	if err == nil || !strings.Contains(err.Error(), "Config.Tenants") {
		t.Fatalf("err = %v, want a missing Config.Tenants error", err)
	}
}

func TestTenantCheck(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		want     connect.Code // 0 for success
	}{
		{"own tenant", "locations/1/things/2", 0},
		{"the tenant itself", "locations/1", 0},
		{"other tenant", "locations/2/things/2", connect.CodePermissionDenied},
		{"across tenants, not granted", "locations/-/things/2", connect.CodePermissionDenied},
		{"no tenant", "things/2", connect.CodeInvalidArgument},
		{"empty", "", connect.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTenantFixture(t, "1")
			err := f.call(t, testsvc.GetTenantThing, f.getThing(t, tt.resource))
			if connect.CodeOf(err) != tt.want && !(tt.want == 0 && err == nil) {
				t.Fatalf("err = %v, want code %v", err, tt.want)
			}
			if tt.want != 0 && *f.handled != 0 {
				t.Error("the handler ran for a denied request")
			}
			// A denial is the guard's answer, never audited as the handler's.
			if strings.Contains(f.log.String(), "error without ErrorInfo") {
				t.Errorf("denial audited: %s", f.log.String())
			}
		})
	}
}

func TestTenantDenialLogged(t *testing.T) {
	f := newTenantFixture(t, "1")
	if err := f.call(t, testsvc.GetTenantThing, f.getThing(t, "locations/2/things/2")); err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(f.log.String(), "tenant check failed") || !strings.Contains(f.log.String(), "tenant=2") {
		t.Fatalf("log = %q", f.log.String())
	}
}

func TestTenantWildcardGranted(t *testing.T) {
	f := newTenantFixture(t, "-")
	if err := f.call(t, testsvc.GetTenantThing, f.getThing(t, "locations/-/things/2")); err != nil {
		t.Fatal(err)
	}
}

func TestTenantNestedField(t *testing.T) {
	f := newTenantFixture(t, "7")
	update := func(name string) proto.Message {
		req := dynamicpb.NewMessage(message(t, f.files, "UpdateThingRequest"))
		thing := req.Mutable(req.Descriptor().Fields().ByName("thing")).Message()
		thing.Set(thing.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString(name))
		return req
	}
	if err := f.call(t, testsvc.UpdateTenantThing, update("locations/7/things/1")); err != nil {
		t.Fatal(err)
	}
	if err := f.call(t, testsvc.UpdateTenantThing, update("locations/8/things/1")); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("err = %v, want permission_denied", err)
	}
}

func TestCrossTenantMethodUnchecked(t *testing.T) {
	f := newTenantFixture(t) // no tenant at all
	req := dynamicpb.NewMessage(message(t, f.files, "ListTenantsRequest"))
	if err := f.call(t, testsvc.ListTenants, req); err != nil {
		t.Fatal(err)
	}
}
