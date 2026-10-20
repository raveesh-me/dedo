package app

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// newMCP serves every RPC as an MCP tool named after it, such as get_todo.
// Tool calls go through api with the caller's token and flow ID, so MCP gets
// the same scopes, flow IDs and audit as the REST and RPC transports, with no
// policy code of its own.
func newMCP(api http.Handler) (http.Handler, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "todo", Version: "v1"}, nil)
	if err := mcpserver.New(server, mcpserver.Config{
		Services:      []protoreflect.ServiceDescriptor{todov1.File_raveesh_me_todo_v1_todo_proto.Services().ByName("TodoService")},
		InputSchemas:  todov1.MCPInputSchemas,
		OutputSchemas: todov1.MCPOutputSchemas,
		Client:        mcpserver.InProcessClient(api),
	}); err != nil {
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
