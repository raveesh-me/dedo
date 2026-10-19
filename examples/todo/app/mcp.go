package app

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	todov1 "github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1mcp"
	"github.com/raveesh-me/http-proto-lint/flowid"
	"github.com/raveesh-me/http-proto-lint/mcp/mcpserver"
)

// newMCP serves every RPC as an MCP tool named after it, such as get_todo. Tool calls go through api with
// the caller's token, so MCP gets the same scopes, flow IDs and audit as the
// REST and RPC transports, with no policy code of its own.
func newMCP(api http.Handler) (http.Handler, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "todo", Version: "v1"}, nil)
	client := todov1connect.NewTodoServiceClient(
		mcpserver.InProcessClient(api),
		"http://in-process",
		connect.WithInterceptors(mcpserver.ForwardHeaders(), flowid.ClientInterceptor()),
	)
	service := todov1.File_raveesh_me_todo_v1_todo_proto.Services().ByName("TodoService")
	tools, err := mcpserver.New(server, mcpserver.Config{
		Services:     []protoreflect.ServiceDescriptor{service},
		InputSchemas: todov1.MCPInputSchemas,
	})
	if err != nil {
		return nil, err
	}
	todov1mcp.ForwardToConnectTodoServiceClient(tools, client)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	), nil
}
