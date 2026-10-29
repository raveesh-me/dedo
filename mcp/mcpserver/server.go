// Package mcpserver serves protobuf services as MCP tools, with the text,
// schemas and behaviour declared in their dedo.v1 options.
//
// AddTools registers the tools protoc-gen-dedo-mcp generates
// (<Service>MCPTools): their definitions are written out at generation time
// and their calls use the generated message types, so nothing is resolved by
// reflection while serving. New does the same from service descriptors at
// run time, with dynamic messages, for services without generated tools.
//
// Both register every unary, non-deprecated RPC as a tool on an MCP Go SDK
// server. A tool's name is the RPC's in snake_case (GetTodo becomes
// get_todo); its title, description, ToolAnnotations and error reasons come
// from the proto options; its input and output schemas are the ones
// protoc-gen-dedo-mcp generated. A tool call is forwarded as a Connect call
// to the service, with the caller's Authorization and X-Flow-ID headers.
// Give them a client of the service's own connect.Server, such as
// connect.NewClient(connectinprocess.New(server)), so MCP calls run through
// the same auth, scope checks, flow IDs and error audit as REST and RPC
// calls. Guard decides who may reach the tools at all.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// forwardedHeaders are copied from the MCP request onto the forwarded call.
var forwardedHeaders = []string{"Authorization", "X-Flow-ID"}

// validName is the tool name format every MCP client and LLM provider accepts.
var validName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,63}$`)

// Config configures New.
type Config struct {
	// Services whose RPCs become tools.
	Services []protoreflect.ServiceDescriptor
	// InputSchemas and OutputSchemas are the generated
	// <Service>MCPInputSchemas and <Service>MCPOutputSchemas of the services
	// (protoc-gen-dedo-mcp), merged. Every tool needs an input schema; an
	// output schema is optional.
	InputSchemas  map[string]string
	OutputSchemas map[string]string
	// Client sends the forwarded calls, usually
	// connect.NewClient(connectinprocess.New(server)): straight to the
	// service's connect.Server, through its interceptors.
	Client *connect.Client
}

// New registers the tools of the services on s. It fails when an RPC has no
// generated input schema, or when two RPCs would get the same tool name,
// which happens only when services sharing a server have RPCs of the same
// name.
func New(s *mcp.Server, cfg Config) error {
	if cfg.Client == nil {
		return errors.New("mcpserver: Config.Client is required")
	}
	owner := map[string]protoreflect.FullName{}
	for _, svc := range cfg.Services {
		for i := 0; i < svc.Methods().Len(); i++ {
			m := svc.Methods().Get(i)
			if !Serves(m) {
				continue
			}
			tool, err := describe(m, cfg)
			if err != nil {
				return err
			}
			if prev, taken := owner[tool.Name]; taken {
				return fmt.Errorf("mcpserver: %s and %s both map to tool %q; serve them from separate MCP servers", prev, m.FullName(), tool.Name)
			}
			owner[tool.Name] = m.FullName()
			// The SDK's typed AddTool validates the arguments against the input
			// schema and the result against the output schema, fills
			// structuredContent, and turns a returned error into a tool error.
			mcp.AddTool(s, tool, forward(m, cfg.Client))
		}
	}
	return nil
}

// describe builds the tool of an RPC from its options and generated schemas.
func describe(m protoreflect.MethodDescriptor, cfg Config) (*mcp.Tool, error) {
	tool, err := Describe(m)
	if err != nil {
		return nil, err
	}
	input, ok := cfg.InputSchemas[string(m.FullName())]
	if !ok {
		return nil, fmt.Errorf("mcpserver: %s has no input schema; generate it with protoc-gen-dedo-mcp", m.FullName())
	}
	tool.InputSchema = json.RawMessage(input)
	if output, ok := cfg.OutputSchemas[string(m.FullName())]; ok {
		tool.OutputSchema = json.RawMessage(output)
	}
	return tool, nil
}

// Serves reports whether an RPC becomes a tool: unary and not deprecated.
func Serves(m protoreflect.MethodDescriptor) bool {
	return !m.IsStreamingClient() && !m.IsStreamingServer() && !isDeprecated(m)
}

// Describe returns the tool of an RPC without its schemas: the name, and
// the title, description, error reasons and ToolAnnotations from its
// options. protoc-gen-dedo-mcp writes the result out as Go code.
func Describe(m protoreflect.MethodDescriptor) (*mcp.Tool, error) {
	name := ToolName(m)
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("mcpserver: %s: tool name %q is not a valid MCP tool name", m.FullName(), name)
	}
	rules, _ := proto.GetExtension(m.Options(), dedov1.E_Method).(*dedov1.MethodRules)
	return &mcp.Tool{
		Name:        name,
		Title:       rules.GetTool().GetTitle(),
		Description: withErrors(rules.GetTool().GetDescription(), rules.GetErrors()),
		Annotations: annotations(rules.GetTool()),
	}, nil
}

// forward returns the tool handler that calls the RPC with dynamic
// messages, so no per-service code is needed.
func forward(m protoreflect.MethodDescriptor, client *connect.Client) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage] {
	spec := connect.Spec{
		Procedure:  fmt.Sprintf("/%s/%s", m.Parent().FullName(), m.Name()),
		StreamType: connect.StreamTypeUnary,
		Schema:     m,
	}
	return func(ctx context.Context, req *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
		in := dynamicpb.NewMessage(m.Input())
		if len(args) > 0 {
			args, err := normalizeMasks(m.Input(), args)
			if err != nil {
				return nil, nil, &ToolError{Code: "INVALID_ARGUMENT", Message: "the arguments are not a JSON object: " + err.Error()}
			}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(args, in); err != nil {
				return nil, nil, &ToolError{Code: "INVALID_ARGUMENT", Message: "the arguments do not match the input schema: " + err.Error()}
			}
		}
		out := dynamicpb.NewMessage(m.Output())
		if err := client.CallUnary(forwardHeaders(ctx, req), spec, in, out); err != nil {
			return nil, nil, toolError(err)
		}
		res, err := marshalResult(out)
		return nil, res, err
	}
}

// ToolError is a failed call as the model sees it: the code, the message,
// and the declared reason from the ErrorInfo detail. Its Error is that JSON,
// which the SDK puts in the tool result.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

func (e *ToolError) Error() string {
	b, _ := json.Marshal(e)
	return string(b)
}

func toolError(err error) *ToolError {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return &ToolError{Code: "UNKNOWN", Message: err.Error()}
	}
	out := &ToolError{Code: strings.ToUpper(cerr.Code().String()), Message: cerr.Message()}
	for _, d := range cerr.Details() {
		if v, derr := connectproto.UnmarshalErrorDetail(d); derr == nil {
			if info, ok := v.(*errdetails.ErrorInfo); ok {
				out.Reason = info.GetReason()
				break
			}
		}
	}
	return out
}

// ToolName returns the MCP tool name of an RPC: its name in snake_case, such
// as get_todo for GetTodo.
func ToolName(m protoreflect.MethodDescriptor) string {
	runes := []rune(string(m.Name()))
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prevLower := unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// withErrors appends the declared error reasons to a tool description, so
// an agent knows the error vocabulary before it hits it.
func withErrors(description string, errors []*dedov1.ErrorReason) string {
	if len(errors) == 0 {
		return description
	}
	reasons := make([]string, 0, len(errors))
	for _, e := range errors {
		reasons = append(reasons, fmt.Sprintf("%s (%s)", e.GetReason(), e.GetCode()))
	}
	return strings.TrimSpace(description + "\n\nErrors: " + strings.Join(reasons, ", ") + ".")
}

// isDeprecated reports whether the RPC sets deprecated = true.
func isDeprecated(m protoreflect.MethodDescriptor) bool {
	opts, _ := m.Options().(*descriptorpb.MethodOptions)
	return opts.GetDeprecated()
}

// annotations returns the ToolAnnotations declared in the proto, as the MCP
// specification (2026-07-28) defines them. An undeclared hint is left out,
// so clients apply the specification's default; the tool annotations lint
// rule makes sure every hint that applies is declared.
func annotations(tool *dedov1.Tool) *mcp.ToolAnnotations {
	a := tool.GetAnnotations()
	if a == nil {
		a = &dedov1.ToolAnnotations{}
	}
	return &mcp.ToolAnnotations{
		Title:           tool.GetTitle(),
		ReadOnlyHint:    a.GetReadOnlyHint(),
		IdempotentHint:  a.GetIdempotentHint(),
		DestructiveHint: copyBool(a.DestructiveHint),
		OpenWorldHint:   copyBool(a.OpenWorldHint),
	}
}
func copyBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}
