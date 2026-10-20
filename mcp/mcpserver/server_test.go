package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// service builds dedo.test.v1.TestService: a read, a destructive write, a
// deprecated RPC and a stream.
func service(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	no, yes := false, true
	method := func(name string, level descriptorpb.MethodOptions_IdempotencyLevel, tool *dedov1.Tool, deprecated, stream bool) *descriptorpb.MethodDescriptorProto {
		opts := &descriptorpb.MethodOptions{IdempotencyLevel: level.Enum(), Deprecated: proto.Bool(deprecated)}
		proto.SetExtension(opts, dedov1.E_Method, &dedov1.MethodRules{
			Tool:   tool,
			Errors: []*dedov1.ErrorReason{{Reason: "THING_NOT_FOUND", Code: code.Code_NOT_FOUND}},
		})
		return &descriptorpb.MethodDescriptorProto{
			Name: proto.String(name), InputType: proto.String(".dedo.test.v1.Request"),
			OutputType: proto.String(".dedo.test.v1.Response"), Options: opts, ServerStreaming: proto.Bool(stream),
		}
	}
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	field := func(name string, n int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(n), Type: str, JsonName: proto.String(name)}
	}
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("dedo/test/v1/test.proto"),
		Package: proto.String("dedo.test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Request"), Field: []*descriptorpb.FieldDescriptorProto{field("name", 1)}},
			{Name: proto.String("Response"), Field: []*descriptorpb.FieldDescriptorProto{field("name", 1), field("note", 2)}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("TestService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("GetThing", descriptorpb.MethodOptions_NO_SIDE_EFFECTS, &dedov1.Tool{
					Title: "Get a thing", Description: "Returns a thing.",
					Annotations: &dedov1.ToolAnnotations{ReadOnlyHint: &yes, OpenWorldHint: &no},
				}, false, false),
				method("DeleteThing", descriptorpb.MethodOptions_IDEMPOTENT, &dedov1.Tool{
					Title: "Delete a thing", Description: "Removes a thing.",
					Annotations: &dedov1.ToolAnnotations{ReadOnlyHint: &no, DestructiveHint: &yes, IdempotentHint: &yes, OpenWorldHint: &no},
				}, false, false),
				method("OldThing", descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN, &dedov1.Tool{Title: "Old", Description: "Old."}, true, false),
				method("WatchThings", descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN, nil, false, true),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Services().Get(0)
}

var (
	inputSchemas = map[string]string{
		"dedo.test.v1.TestService.GetThing":    `{"type":"object","properties":{"name":{"type":"string","description":"Which thing."}}}`,
		"dedo.test.v1.TestService.DeleteThing": `{"type":"object","properties":{"name":{"type":"string"}}}`,
	}
	outputSchemas = map[string]string{
		"dedo.test.v1.TestService.GetThing": `{"type":"object","properties":{"name":{"type":"string"},"note":{"type":"string"}}}`,
	}
)

// backend serves GetThing and DeleteThing with dynamic messages, in-process.
// GetThing echoes the name and the Authorization header it got, and fails
// with THING_NOT_FOUND for the name "missing".
func backend(svc protoreflect.ServiceDescriptor) *connect.Client {
	server := connect.NewServer()
	for _, name := range []protoreflect.Name{"GetThing", "DeleteThing"} {
		m := svc.Methods().ByName(name)
		server.Register(connect.Method{
			Spec: connect.Spec{Procedure: "/dedo.test.v1.TestService/" + string(name), StreamType: connect.StreamTypeUnary, Schema: m},
			Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
				req := dynamicpb.NewMessage(m.Input())
				if err := stream.Receive(req); err != nil {
					return err
				}
				thing := req.Get(m.Input().Fields().ByName("name")).String()
				if thing == "missing" {
					detail, _ := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: "THING_NOT_FOUND"})
					return connect.NewError(connect.CodeNotFound, "no such thing").WithDetail(detail)
				}
				info, _ := connect.CallInfoForServerContext(ctx)
				out := dynamicpb.NewMessage(m.Output())
				out.Set(m.Output().Fields().ByName("name"), protoreflect.ValueOfString(thing))
				out.Set(m.Output().Fields().ByName("note"), protoreflect.ValueOfString(info.RequestHeader().Get("Authorization")))
				return stream.Send(out)
			},
		})
	}
	return connect.NewClient(connectinprocess.New(server))
}

// bearer adds a token to every request of the MCP client.
type bearer struct{ token string }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	svc := service(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	if err := New(server, Config{
		Services: []protoreflect.ServiceDescriptor{svc}, InputSchemas: inputSchemas, OutputSchemas: outputSchemas,
		Client: backend(svc),
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	t.Cleanup(ts.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL, HTTPClient: &http.Client{Transport: bearer{token: "t"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// Unary, non-deprecated RPCs become tools with the declared text and schemas.
func TestTools(t *testing.T) {
	res, err := session(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	if len(tools) != 2 || tools["get_thing"] == nil || tools["delete_thing"] == nil {
		t.Fatalf("tools = %v, want get_thing and delete_thing only", res.Tools)
	}
	get := tools["get_thing"]
	if get.Title != "Get a thing" || get.Description != "Returns a thing.\n\nErrors: THING_NOT_FOUND (NOT_FOUND)." {
		t.Errorf("get_thing: title %q, description %q", get.Title, get.Description)
	}
	in, _ := json.Marshal(get.InputSchema)
	out, _ := json.Marshal(get.OutputSchema)
	if !strings.Contains(string(in), "Which thing.") || !strings.Contains(string(out), "note") {
		t.Errorf("get_thing schemas: in %s, out %s", in, out)
	}
	if a := tools["delete_thing"].Annotations; a.ReadOnlyHint || !*a.DestructiveHint || !a.IdempotentHint {
		t.Errorf("delete_thing annotations = %+v", a)
	}
}

// A call is forwarded over Connect with the caller's token, and the
// response comes back as structured content.
func TestForwardsCalls(t *testing.T) {
	res, err := session(t).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_thing", Arguments: map[string]any{"name": "a"},
	})
	if err != nil || res.IsError {
		t.Fatalf("call: %v %s", err, text(res))
	}
	got, _ := json.Marshal(res.StructuredContent)
	if string(got) != `{"name":"a","note":"Bearer t"}` {
		t.Errorf("structured content = %s", got)
	}
}

// A failed call is a tool error with the code and the declared reason.
func TestForwardsErrors(t *testing.T) {
	cs := session(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_thing", Arguments: map[string]any{"name": "missing"},
	})
	if err != nil || !res.IsError || text(res) != `{"code":"NOT_FOUND","message":"no such thing","reason":"THING_NOT_FOUND"}` {
		t.Errorf("missing thing: %v isError=%v %s", err, res.IsError, text(res))
	}
	// The SDK validates arguments against the input schema before the call.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_thing", Arguments: map[string]any{"name": 7},
	})
	if err != nil || !res.IsError || !strings.Contains(text(res), "validating") {
		t.Errorf("bad arguments: %v %s", err, text(res))
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	svc := service(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	services := []protoreflect.ServiceDescriptor{svc}
	client := backend(svc)
	if err := New(server, Config{Services: services, InputSchemas: inputSchemas}); err == nil {
		t.Error("want an error without a client")
	}
	if err := New(server, Config{Services: services, Client: client}); err == nil || !strings.Contains(err.Error(), "protoc-gen-dedo-mcp") {
		t.Errorf("missing schema: %v", err)
	}
	if err := New(server, Config{Services: []protoreflect.ServiceDescriptor{svc, svc}, InputSchemas: inputSchemas, Client: client}); err == nil {
		t.Error("want an error for duplicate tool names")
	}
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
