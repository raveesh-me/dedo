package app

import (
	"net/http"

	"connectrpc.com/connect/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	contactsv1 "github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// NewMCP serves every RPC as an MCP tool named after it, such as
// get_contact. Tool calls go to the server through client, with the
// caller's token and flow ID, so MCP gets the same interceptors as Connect,
// gRPC and REST, the tenant check included, with no policy code of its own.
func NewMCP(client *connect.Client) (http.Handler, error) {
	tools := mcp.NewServer(&mcp.Implementation{Name: "contacts", Version: "v1"}, nil)
	if err := mcpserver.AddTools(tools, client, contactsv1.ContactServiceMCPTools()...); err != nil {
		return nil, err
	}
	if err := mcpserver.AddTools(tools, client, contactsv1.LocationServiceMCPTools()...); err != nil {
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
