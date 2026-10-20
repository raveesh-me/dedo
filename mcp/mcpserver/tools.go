package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
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
type Call func(target Target) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage]

// Target is where tool calls are forwarded, as Connect calls.
type Target struct {
	// Client sends them, usually InProcessClient(api): the service's whole
	// HTTP stack, so MCP calls get the same auth, scope checks, flow IDs and
	// error audit as REST and RPC calls.
	Client *http.Client
	// BaseURL defaults to "http://in-process".
	BaseURL string
}

// AddTools registers generated tools on s. It fails when the client is
// missing or two tools share a name, which happens only when services
// sharing a server have RPCs of the same name.
func AddTools(s *mcp.Server, target Target, tools ...Tool) error {
	if target.Client == nil {
		return errors.New("mcpserver: Target.Client is required")
	}
	target.BaseURL = strings.TrimSuffix(target.BaseURL, "/")
	if target.BaseURL == "" {
		target.BaseURL = "http://in-process"
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
		mcp.AddTool(s, t.Tool, t.Call(target))
	}
	return nil
}

// Unary returns the forwarder of a unary RPC with request type Req and
// response type Res, at its Connect procedure path, such as
// "/acme.time_counter.v1.CounterService/AddTime". Generated code calls it;
// the messages are the generated types, so nothing is resolved at run time.
func Unary[Req, Res any, PReq interface {
	*Req
	proto.Message
}, PRes interface {
	*Res
	proto.Message
}](procedure string) Call {
	return func(target Target) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage] {
		client := connect.NewClient[Req, Res](target.Client, target.BaseURL+procedure)
		desc := PReq(new(Req)).ProtoReflect().Descriptor()
		hasMask := hasFieldMask(desc, map[protoreflect.FullName]bool{})
		return func(ctx context.Context, req *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
			in := PReq(new(Req))
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
			call := connect.NewRequest((*Req)(in))
			copyHeaders(req, call.Header())
			res, err := client.CallUnary(ctx, call)
			if err != nil {
				return nil, nil, toolError(err)
			}
			out, err := marshalResult(PRes(res.Msg))
			return nil, out, err
		}
	}
}

// copyHeaders forwards the caller's token and flow ID.
func copyHeaders(req *mcp.CallToolRequest, h http.Header) {
	if req.Extra == nil {
		return
	}
	for _, name := range forwardedHeaders {
		if v := req.Extra.Header.Get(name); v != "" {
			h.Set(name, v)
		}
	}
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
