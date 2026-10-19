// Package testsvc builds an in-memory service descriptor with declared
// options, for tests of the runtime packages.
package testsvc

import (
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// Service is the full name of the test service.
const Service = "raveesh_me.test.v1.TestService"

// Procedures of the test service.
const (
	GetThing    = "/" + Service + "/GetThing"
	DeleteThing = "/" + Service + "/DeleteThing"
)

// Scopes declared by the test service.
const (
	ReadScope  = "test.thing.read"
	WriteScope = "test.thing.write"
)

// Reason declared by GetThing with NOT_FOUND.
const Reason = "THING_NOT_FOUND"

// Deprecation and sunset times of DeleteThing, in Unix seconds.
const (
	DeprecatedAt = 1767225600 // 2026-01-01T00:00:00Z
	SunsetAt     = 1798761600 // 2027-01-01T00:00:00Z
)

// Edit changes the descriptor before it is registered.
type Edit func(*descriptorpb.FileDescriptorProto)

// Files returns a registry holding the test service, after applying edits.
func Files(edits ...Edit) (*protoregistry.Files, error) {
	fdp := file()
	for _, edit := range edits {
		edit(fdp)
	}
	empty := protodesc.ToFileDescriptorProto(emptypb.File_google_protobuf_empty_proto)
	return protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
			empty,
			fdp,
		},
	})
}

// WithoutServiceRules removes the service option.
func WithoutServiceRules(f *descriptorpb.FileDescriptorProto) {
	f.Service[0].Options = nil
}

// WithoutScopes removes the scopes of DeleteThing.
func WithoutScopes(f *descriptorpb.FileDescriptorProto) {
	rules := proto.GetExtension(f.Service[0].Method[1].Options, dedov1.E_Method).(*dedov1.MethodRules)
	rules.Scopes = nil
}

func file() *descriptorpb.FileDescriptorProto {
	empty := ".google.protobuf.Empty"
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String("raveesh_me/test/v1/test.proto"),
		Package:    proto.String("raveesh_me.test.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/empty.proto"},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:    proto.String("TestService"),
			Options: serviceOptions(),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("GetThing"), InputType: &empty, OutputType: &empty, Options: getOptions()},
				{Name: proto.String("DeleteThing"), InputType: &empty, OutputType: &empty, Options: deleteOptions()},
			},
		}},
	}
}

func serviceOptions() *descriptorpb.ServiceOptions {
	opts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(opts, dedov1.E_Service, &dedov1.ServiceRules{
		Audience:  dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC,
		OwnerTeam: "test-team",
	})
	return opts
}

func getOptions() *descriptorpb.MethodOptions {
	opts := &descriptorpb.MethodOptions{
		IdempotencyLevel: descriptorpb.MethodOptions_NO_SIDE_EFFECTS.Enum(),
	}
	proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
		Scopes: []string{ReadScope},
		Cache: &dedov1.CachePolicy{
			Visibility: dedov1.CachePolicy_VISIBILITY_PUBLIC,
			MaxAge:     durationpb.New(30_000_000_000),
		},
		Errors: []*dedov1.ErrorReason{{Reason: Reason, Code: code.Code_NOT_FOUND}},
	})
	return opts
}

func deleteOptions() *descriptorpb.MethodOptions {
	opts := &descriptorpb.MethodOptions{Deprecated: proto.Bool(true)}
	proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
		Scopes: []string{WriteScope},
		Deprecation: &dedov1.Deprecation{
			DeprecationTime: &timestamppb.Timestamp{Seconds: DeprecatedAt},
			SunsetTime:      &timestamppb.Timestamp{Seconds: SunsetAt},
			Successor:       "raveesh_me.test.v2.TestService.DeleteThing",
		},
		Errors: []*dedov1.ErrorReason{{Reason: Reason, Code: code.Code_NOT_FOUND}},
	})
	return opts
}
