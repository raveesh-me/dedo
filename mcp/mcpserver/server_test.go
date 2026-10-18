package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
)

// service builds raveesh_me.test.v1.TestService with one read and one
// destructive method.
func service(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	method := func(name string, level descriptorpb.MethodOptions_IdempotencyLevel, tool *hplv1.Tool) *descriptorpb.MethodDescriptorProto {
		opts := &descriptorpb.MethodOptions{IdempotencyLevel: level.Enum()}
		proto.SetExtension(opts, hplv1.E_Method, &hplv1.MethodRules{Tool: tool})
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(name), InputType: proto.String(".raveesh_me.test.v1.Request"),
			OutputType: proto.String(".raveesh_me.test.v1.Request"), Options: opts,
		}
	}
	msgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts, hplv1.E_Message, &hplv1.MessageRules{Description: "What to act on."})
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        proto.String("raveesh_me/test/v1/test.proto"),
		Package:     proto.String("raveesh_me.test.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Request"), Options: msgOpts}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("GetThing", descriptorpb.MethodOptions_NO_SIDE_EFFECTS, &hplv1.Tool{Title: "Get a thing", Description: "Returns a thing."}),
				method("DeleteThing", descriptorpb.MethodOptions_IDEMPOTENT, &hplv1.Tool{Title: "Delete a thing", Description: "Removes a thing.", Destructive: true}),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services().Get(0)
}

func TestAnnotations(t *testing.T) {
	svc := service(t)
	get := svc.Methods().ByName("GetThing")
	getRules, _ := proto.GetExtension(get.Options(), hplv1.E_Method).(*hplv1.MethodRules)
	a := annotations(get, getRules)
	if !a.ReadOnlyHint || !a.IdempotentHint || *a.DestructiveHint || *a.OpenWorldHint || a.Title != "Get a thing" {
		t.Errorf("GetThing annotations = %+v", a)
	}
	del := svc.Methods().ByName("DeleteThing")
	delRules, _ := proto.GetExtension(del.Options(), hplv1.E_Method).(*hplv1.MethodRules)
	a = annotations(del, delRules)
	if a.ReadOnlyHint || !a.IdempotentHint || !*a.DestructiveHint {
		t.Errorf("DeleteThing annotations = %+v", a)
	}
}

func TestDescribeSchema(t *testing.T) {
	input := service(t).Methods().Get(0).Input()
	out := describeSchema(json.RawMessage(`{"type":"object"}`), input)
	var schema map[string]any
	if err := json.Unmarshal(out, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["description"] != "What to act on." || schema["type"] != "object" {
		t.Errorf("schema = %v", schema)
	}
	if got := describeSchema(json.RawMessage(`not json`), input); string(got) != "not json" {
		t.Errorf("malformed schema changed: %s", got)
	}
}

// ForwardHeaders and InProcessClient carry the MCP caller's headers into
// the in-process call.
func TestForwardHeadersInProcess(t *testing.T) {
	var seen http.Header
	mux := http.NewServeMux()
	mux.Handle("/test.v1.S/M", connect.NewUnaryHandler("/test.v1.S/M",
		func(_ context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			seen = req.Header()
			return connect.NewResponse(&emptypb.Empty{}), nil
		}))
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](InProcessClient(mux), "http://in-process/test.v1.S/M",
		connect.WithInterceptors(ForwardHeaders()))

	inbound := http.Header{}
	inbound.Set("Authorization", "Bearer t")
	inbound.Set("X-Flow-ID", "flow-1")
	inbound.Set("Cookie", "not forwarded")
	if _, err := client.CallUnary(withHeaders(context.Background(), inbound), connect.NewRequest(&emptypb.Empty{})); err != nil {
		t.Fatal(err)
	}
	if seen.Get("Authorization") != "Bearer t" || seen.Get("X-Flow-ID") != "flow-1" || seen.Get("Cookie") != "" {
		t.Errorf("forwarded headers = %v", seen)
	}
}
