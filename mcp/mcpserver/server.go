// Package mcpserver serves protobuf services as MCP tools, with the text,
// schemas and behaviour declared in their dedo.v1 options.
//
// New registers every unary, non-deprecated RPC as a tool on an MCP Go SDK
// server. A tool's name is the RPC's in snake_case (GetTodo becomes
// get_todo); its title, description, ToolAnnotations and error reasons come
// from the proto options; its input and output schemas are the ones
// protoc-gen-dedo-mcp generated. A tool call is forwarded as a Connect call
// to the service, with the caller's Authorization and X-Flow-ID headers.
// Give Config.Client the service's whole HTTP stack (see InProcessClient),
// so MCP calls run through the same auth, scope checks, flow IDs and error
// audit as REST and RPC calls.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"connectrpc.com/connect"
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
	// InputSchemas and OutputSchemas are the generated MCPInputSchemas and
	// MCPOutputSchemas of the services' proto files (protoc-gen-dedo-mcp),
	// merged. Every tool needs an input schema; an output schema is optional.
	InputSchemas  map[string]string
	OutputSchemas map[string]string
	// Client sends the forwarded Connect calls, usually InProcessClient(api).
	Client *http.Client
	// BaseURL is where Client sends them. It defaults to "http://in-process".
	BaseURL string
}

// New registers the tools of the services on s. It fails when an RPC has no
// generated input schema, or when two RPCs would get the same tool name,
// which happens only when services sharing a server have RPCs of the same
// name.
func New(s *mcp.Server, cfg Config) error {
	if cfg.Client == nil {
		return errors.New("mcpserver: Config.Client is required")
	}
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://in-process"
	}
	owner := map[string]protoreflect.FullName{}
	for _, svc := range cfg.Services {
		for i := 0; i < svc.Methods().Len(); i++ {
			m := svc.Methods().Get(i)
			if m.IsStreamingClient() || m.IsStreamingServer() || isDeprecated(m) {
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
			mcp.AddTool(s, tool, forward(m, cfg.Client, baseURL))
		}
	}
	return nil
}

// describe builds the tool of an RPC from its options and generated schemas.
func describe(m protoreflect.MethodDescriptor, cfg Config) (*mcp.Tool, error) {
	name := ToolName(m)
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("mcpserver: %s: tool name %q is not a valid MCP tool name", m.FullName(), name)
	}
	input, ok := cfg.InputSchemas[string(m.FullName())]
	if !ok {
		return nil, fmt.Errorf("mcpserver: %s has no input schema; generate it with protoc-gen-dedo-mcp", m.FullName())
	}
	rules, _ := proto.GetExtension(m.Options(), dedov1.E_Method).(*dedov1.MethodRules)
	tool := &mcp.Tool{
		Name:        name,
		Title:       rules.GetTool().GetTitle(),
		Description: withErrors(rules.GetTool().GetDescription(), rules.GetErrors()),
		InputSchema: json.RawMessage(input),
		Annotations: annotations(rules.GetTool()),
	}
	if output, ok := cfg.OutputSchemas[string(m.FullName())]; ok {
		tool.OutputSchema = json.RawMessage(output)
	}
	return tool, nil
}

// forward returns the tool handler that calls the RPC over Connect with
// dynamic messages, so no per-service code is needed.
func forward(m protoreflect.MethodDescriptor, httpClient *http.Client, baseURL string) mcp.ToolHandlerFor[json.RawMessage, map[string]any] {
	opts := []connect.ClientOption{
		connect.WithSchema(m),
		connect.WithResponseInitializer(func(_ connect.Spec, msg any) error {
			if dynamic, ok := msg.(*dynamicpb.Message); ok {
				*dynamic = *dynamicpb.NewMessage(m.Output())
			}
			return nil
		}),
	}
	procedure := fmt.Sprintf("/%s/%s", m.Parent().FullName(), m.Name())
	client := connect.NewClient[dynamicpb.Message, dynamicpb.Message](httpClient, baseURL+procedure, opts...)
	return func(ctx context.Context, req *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, map[string]any, error) {
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
		call := connect.NewRequest(in)
		if req.Extra != nil {
			for _, h := range forwardedHeaders {
				if v := req.Extra.Header.Get(h); v != "" {
					call.Header().Set(h, v)
				}
			}
		}
		res, err := client.CallUnary(ctx, call)
		if err != nil {
			return nil, nil, toolError(err)
		}
		// Default values are written out, so an empty list is
		// {"todos":[],"next_page_token":""} rather than {}, which a model
		// cannot tell from a broken reply.
		out, err := protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}.Marshal(res.Msg)
		if err != nil {
			return nil, nil, err
		}
		var structured map[string]any
		if err := json.Unmarshal(out, &structured); err != nil {
			return nil, nil, err
		}
		return nil, structured, nil
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
		if v, derr := d.Value(); derr == nil {
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
