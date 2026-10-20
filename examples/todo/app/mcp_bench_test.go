package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// BenchmarkMCPCall compares one get_todo tool call through the generated
// tools (mcpserver.AddTools) and through the reflective path (mcpserver.New),
// end to end over an in-memory MCP session.
func BenchmarkMCPCall(b *testing.B) {
	api, err := newServer(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		b.Fatal(err)
	}
	client := connect.NewClient(connectinprocess.New(api))
	paths := map[string]func(*mcp.Server) error{
		"generated": func(s *mcp.Server) error {
			return mcpserver.AddTools(s, client, todov1.TodoServiceMCPTools()...)
		},
		"reflective": func(s *mcp.Server) error {
			return mcpserver.New(s, mcpserver.Config{
				Services:      []protoreflect.ServiceDescriptor{todov1.File_raveesh_me_todo_v1_todo_proto.Services().ByName("TodoService")},
				InputSchemas:  todov1.MCPInputSchemas,
				OutputSchemas: todov1.MCPOutputSchemas,
				Client:        client,
			})
		},
	}
	for _, name := range []string{"generated", "reflective"} {
		b.Run(name, func(b *testing.B) {
			ctx := context.Background()
			server := mcp.NewServer(&mcp.Implementation{Name: "bench", Version: "v1"}, nil)
			if err := paths[name](server); err != nil {
				b.Fatal(err)
			}
			st, ct := mcp.NewInMemoryTransports()
			if _, err := server.Connect(ctx, st, nil); err != nil {
				b.Fatal(err)
			}
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v1"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer cs.Close()
			created, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_todo", Arguments: map[string]any{
				"todo_id": "bench", "todo": map[string]any{"title": "benchmark", "budget_amount": map[string]any{"currency_code": "EUR", "units": "5"}},
			}})
			// The sub-benchmarks share one store and run more than once.
			if err != nil || created.IsError && !strings.Contains(created.Content[0].(*mcp.TextContent).Text, "ALREADY_EXISTS") {
				b.Fatalf("create_todo: %v %+v", err, created)
			}
			args := &mcp.CallToolParams{Name: "get_todo", Arguments: map[string]any{"name": "todos/bench"}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				res, err := cs.CallTool(ctx, args)
				if err != nil || res.IsError {
					b.Fatalf("get_todo: %v %+v", err, res)
				}
			}
		})
	}
}
