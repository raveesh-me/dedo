package app

import (
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	todov1 "github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1mcp"
	"github.com/raveesh-me/http-proto-lint/flowid"
	"github.com/raveesh-me/http-proto-lint/mcp/mcpserver"
)

// newMCP serves every RPC as an MCP tool. Tool calls go through api with
// the caller's token, so MCP gets the same scopes, flow IDs and audit as the
// REST and RPC transports, with no policy code of its own.
func newMCP(api http.Handler) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "todo", Version: "v1"}, nil)
	client := todov1connect.NewTodoServiceClient(
		mcpserver.InProcessClient(api),
		"http://in-process",
		connect.WithInterceptors(mcpserver.ForwardHeaders(), flowid.ClientInterceptor()),
	)
	service := todov1.File_raveesh_me_todo_v1_todo_proto.Services().ByName("TodoService")
	todov1mcp.ForwardToConnectTodoServiceClient(mcpserver.Wrap(server, service), client)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}
