package testsvc

import (
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// TenantService is the full name of the multi-tenant test service, whose
// tenants are "locations".
const TenantService = "raveesh_me.tenant.v1.TenantService"

// Procedures of the multi-tenant test service.
const (
	GetTenantThing    = "/" + TenantService + "/GetThing"    // {name=locations/*/things/*}
	UpdateTenantThing = "/" + TenantService + "/UpdateThing" // {thing.name=locations/*/things/*}
	ListTenants       = "/" + TenantService + "/ListTenants" // cross-tenant
)

// TenantEdit changes the multi-tenant descriptor before it is registered.
type TenantEdit func(*descriptorpb.FileDescriptorProto)

// TenantFiles returns a registry holding the multi-tenant test service,
// after applying edits.
func TenantFiles(edits ...TenantEdit) (*protoregistry.Files, error) {
	fdp := tenantFile()
	for _, edit := range edits {
		edit(fdp)
	}
	return protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
			fdp,
		},
	})
}

// WithHTTPGet sets the HTTP binding of GetThing to one GET path.
func WithHTTPGet(path string) TenantEdit {
	return func(f *descriptorpb.FileDescriptorProto) {
		proto.SetExtension(f.Service[0].Method[0].Options, annotations.E_Http,
			&annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: path}})
	}
}

// WithAdditionalGet adds an HTTP binding to GetThing.
func WithAdditionalGet(path string) TenantEdit {
	return func(f *descriptorpb.FileDescriptorProto) {
		rule := proto.GetExtension(f.Service[0].Method[0].Options, annotations.E_Http).(*annotations.HttpRule)
		rule.AdditionalBindings = append(rule.AdditionalBindings,
			&annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: path}})
	}
}

// WithoutHTTP removes the HTTP binding of GetThing.
func WithoutHTTP(f *descriptorpb.FileDescriptorProto) {
	proto.ClearExtension(f.Service[0].Method[0].Options, annotations.E_Http)
}

func tenantFile() *descriptorpb.FileDescriptorProto {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	msg := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	field := func(name string, number int32, typ *descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(number), Label: optional, Type: typ, JsonName: proto.String(name)}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}
	message := func(name string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
		return &descriptorpb.DescriptorProto{Name: proto.String(name), Field: fields}
	}
	method := func(name, in, out string, opts *descriptorpb.MethodOptions) *descriptorpb.MethodDescriptorProto {
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(name), InputType: proto.String(".raveesh_me.tenant.v1." + in),
			OutputType: proto.String(".raveesh_me.tenant.v1." + out), Options: opts,
		}
	}
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("raveesh_me/tenant/v1/tenant.proto"),
		Package: proto.String("raveesh_me.tenant.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			message("Thing", field("name", 1, str, "")),
			message("GetThingRequest", field("name", 1, str, "")),
			message("UpdateThingRequest", field("thing", 1, msg, ".raveesh_me.tenant.v1.Thing")),
			message("ListTenantsRequest"),
			message("ListTenantsResponse"),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TenantService"),
			Options: func() *descriptorpb.ServiceOptions {
				opts := &descriptorpb.ServiceOptions{}
				proto.SetExtension(opts, dedov1.E_Service, &dedov1.ServiceRules{
					Audience:  dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC,
					OwnerTeam: "test-team",
					Tenancy:   &dedov1.Tenancy{Collection: "locations"},
				})
				return opts
			}(),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("GetThing", "GetThingRequest", "Thing",
					tenantMethodOptions(&annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/v1/{name=locations/*/things/*}"}}, false)),
				method("UpdateThing", "UpdateThingRequest", "Thing",
					tenantMethodOptions(&annotations.HttpRule{Pattern: &annotations.HttpRule_Patch{Patch: "/v1/{thing.name=locations/*/things/*}"}, Body: "thing"}, false)),
				method("ListTenants", "ListTenantsRequest", "ListTenantsResponse",
					tenantMethodOptions(&annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/v1/locations"}}, true)),
			},
		}},
	}
}

func tenantMethodOptions(rule *annotations.HttpRule, crossTenant bool) *descriptorpb.MethodOptions {
	opts := &descriptorpb.MethodOptions{}
	proto.SetExtension(opts, annotations.E_Http, rule)
	proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
		Scopes:      []string{ReadScope},
		CrossTenant: crossTenant,
	})
	return opts
}
