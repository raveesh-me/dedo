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

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// service builds raveesh_me.test.v1.TestService with one read and one
// destructive method.
func service(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	method := func(name string, level descriptorpb.MethodOptions_IdempotencyLevel, tool *dedov1.Tool) *descriptorpb.MethodDescriptorProto {
		opts := &descriptorpb.MethodOptions{IdempotencyLevel: level.Enum()}
		proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{Tool: tool})
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(name), InputType: proto.String(".raveesh_me.test.v1.Request"),
			OutputType: proto.String(".raveesh_me.test.v1.Request"), Options: opts,
		}
	}
	msgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts, dedov1.E_Message, &dedov1.MessageRules{Description: "What to act on."})
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        proto.String("raveesh_me/test/v1/test.proto"),
		Package:     proto.String("raveesh_me.test.v1"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Request"), Options: msgOpts}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("GetThing", descriptorpb.MethodOptions_NO_SIDE_EFFECTS, &dedov1.Tool{Title: "Get a thing", Description: "Returns a thing."}),
				method("DeleteThing", descriptorpb.MethodOptions_IDEMPOTENT, &dedov1.Tool{Title: "Delete a thing", Description: "Removes a thing."}),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services().Get(0)
}

func TestAnnotations(t *testing.T) {
	no, yes := false, true
	a := annotations(&dedov1.Tool{
		Title: "Delete a thing",
		Annotations: &dedov1.ToolAnnotations{
			ReadOnlyHint: &no, DestructiveHint: &yes, IdempotentHint: &yes, OpenWorldHint: &no,
		},
	})
	if a.Title != "Delete a thing" || a.ReadOnlyHint || !a.IdempotentHint || !*a.DestructiveHint || *a.OpenWorldHint {
		t.Errorf("declared annotations = %+v", a)
	}
	// Undeclared hints are left out, so clients apply the spec defaults.
	a = annotations(&dedov1.Tool{Title: "Get a thing", Annotations: &dedov1.ToolAnnotations{ReadOnlyHint: &yes}})
	if !a.ReadOnlyHint || a.DestructiveHint != nil || a.OpenWorldHint != nil {
		t.Errorf("partial annotations = %+v", a)
	}
	if a = annotations(nil); a.ReadOnlyHint || a.DestructiveHint != nil || a.OpenWorldHint != nil {
		t.Errorf("no tool = %+v", a)
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

func TestToolName(t *testing.T) {
	svc := service(t)
	if got := ToolName(svc.Methods().ByName("GetThing")); got != "get_thing" {
		t.Errorf("ToolName(GetThing) = %q", got)
	}
	for in, want := range map[string]string{
		"GetHTTPRule": "get_http_rule", "ListV2Items": "list_v2_items", "Echo": "echo", "BatchGetTodos": "batch_get_todos",
	} {
		m := methodNamed(t, in)
		if got := ToolName(m); got != want {
			t.Errorf("ToolName(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapRejectsDuplicateToolNames(t *testing.T) {
	svc := service(t)
	if _, err := Wrap(nil, svc); err != nil {
		t.Fatalf("one service: %v", err)
	}
	if _, err := Wrap(nil, svc, svc); err == nil {
		t.Fatal("want an error when two RPCs map to the same tool name")
	}
}

// methodNamed returns a method descriptor with the given name.
func methodNamed(t *testing.T, name string) protoreflect.MethodDescriptor {
	t.Helper()
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:        proto.String("n/" + name + ".proto"),
		Package:     proto.String("n"),
		Syntax:      proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("M")}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:   proto.String("S"),
			Method: []*descriptorpb.MethodDescriptorProto{{Name: proto.String(name), InputType: proto.String(".n.M"), OutputType: proto.String(".n.M")}},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services().Get(0).Methods().Get(0)
}
