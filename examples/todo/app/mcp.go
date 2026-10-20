package app

import (
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// newMCP serves every RPC as an MCP tool named after it, such as get_todo.
// Tool calls go straight to the server, in-process, with the caller's token
// and flow ID, so MCP gets the same interceptors as Connect, gRPC and REST,
// with no policy code of its own.
func newMCP(server *connect.Server) (http.Handler, error) {
	tools := mcp.NewServer(&mcp.Implementation{Name: "todo", Version: "v1"}, nil)
	client := connect.NewClient(connectinprocess.New(server))
	if err := mcpserver.AddTools(tools, client, todov1.TodoServiceMCPTools()...); err != nil {
		return nil, err
	}
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return tools },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	// The MCP transport requires servers to check Origin, so a web page on
	// another site cannot drive the tools (DNS rebinding). MCP clients send
	// no Origin and pass.
	return http.NewCrossOriginProtection().Handler(handler), nil
}
