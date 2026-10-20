package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Tool is one RPC as an MCP tool, as protoc-gen-dedo-mcp generates it: the
// tool's definition, written out at generation time, and its typed
// forwarder. <Service>MCPTools in the generated code returns them.
type Tool struct {
	Tool *mcp.Tool
	Call Call
}

// Call binds a tool to the client that forwards its calls.
type Call func(client *connect.Client) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage]

// AddTools registers generated tools on s, each forwarding its calls through
// client, usually connect.NewClient(connectinprocess.New(server)): straight
// to the service's connect.Server, so MCP calls get the same auth, scope
// checks, validation, flow IDs and error audit as Connect, gRPC and REST
// calls. It fails when the client is missing or two tools share a name,
// which happens only when services sharing a server have RPCs of the same
// name.
func AddTools(s *mcp.Server, client *connect.Client, tools ...Tool) error {
	if client == nil {
		return errors.New("mcpserver: a client is required")
	}
	seen := map[string]bool{}
	for _, t := range tools {
		if t.Tool == nil || t.Call == nil {
			return errors.New("mcpserver: a tool lacks its definition or its call")
		}
		if !validName.MatchString(t.Tool.Name) {
			return fmt.Errorf("mcpserver: tool name %q is not a valid MCP tool name", t.Tool.Name)
		}
		if seen[t.Tool.Name] {
			return fmt.Errorf("mcpserver: two tools are named %q; serve their services from separate MCP servers", t.Tool.Name)
		}
		seen[t.Tool.Name] = true
		mcp.AddTool(s, t.Tool, t.Call(client))
	}
	return nil
}

// Unary returns the forwarder of a unary RPC at its procedure, such as
// "/acme.time_counter.v1.CounterService/AddTime". newRequest and newResponse
// return empty generated messages, so nothing is resolved at run time.
// Generated code calls it once per RPC; it is not generic, so tools add no
// per-RPC copy of this code to a binary.
func Unary(procedure string, newRequest, newResponse func() proto.Message) Call {
	spec := connect.Spec{Procedure: procedure, StreamType: connect.StreamTypeUnary}
	return func(client *connect.Client) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage] {
		desc := newRequest().ProtoReflect().Descriptor()
		hasMask := hasFieldMask(desc, map[protoreflect.FullName]bool{})
		return func(ctx context.Context, req *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
			in := newRequest()
			if len(args) > 0 {
				if hasMask {
					var err error
					if args, err = normalizeMasks(desc, args); err != nil {
						return nil, nil, &ToolError{Code: "INVALID_ARGUMENT", Message: "the arguments are not a JSON object: " + err.Error()}
					}
				}
				if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(args, in); err != nil {
					return nil, nil, &ToolError{Code: "INVALID_ARGUMENT", Message: "the arguments do not match the input schema: " + err.Error()}
				}
			}
			out := newResponse()
			if err := client.CallUnary(forwardHeaders(ctx, req), spec, in, out); err != nil {
				return nil, nil, toolError(err)
			}
			res, err := marshalResult(out)
			return nil, res, err
		}
	}
}

// forwardHeaders puts the MCP caller's token and flow ID on the forwarded
// call, where the server's interceptors read them.
func forwardHeaders(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req == nil || req.Extra == nil {
		return ctx
	}
	ctx, info := connect.NewClientContext(ctx)
	for _, name := range forwardedHeaders {
		if v := req.Extra.Header.Get(name); v != "" {
			info.RequestHeader().Set(name, v)
		}
	}
	return ctx
}

// marshalResult writes a response as structuredContent: proto field names,
// and default values written out, so an empty list is
// {"todos":[],"next_page_token":""} rather than {}, which a model cannot
// tell from a broken reply.
func marshalResult(m proto.Message) (json.RawMessage, error) {
	return protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}.Marshal(m)
}

// Bool returns a pointer to b, for generated ToolAnnotations.
func Bool(b bool) *bool { return &b }
