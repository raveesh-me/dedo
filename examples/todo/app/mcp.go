package app

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// newMCP serves every RPC as an MCP tool named after it, such as get_todo.
// Tool calls go through api with the caller's token and flow ID, so MCP gets
// the same scopes, flow IDs and audit as the REST and RPC transports, with no
// policy code of its own.
func newMCP(api http.Handler) (http.Handler, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "todo", Version: "v1"}, nil)
	target := mcpserver.Target{Client: mcpserver.InProcessClient(api)}
	if err := mcpserver.AddTools(server, target, todov1.TodoServiceMCPTools()...); err != nil {
		return nil, err
	}
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	// The MCP transport requires servers to check Origin, so a web page on
	// another site cannot drive the tools (DNS rebinding). MCP clients send
	// no Origin and pass.
	return http.NewCrossOriginProtection().Handler(handler), nil
}
