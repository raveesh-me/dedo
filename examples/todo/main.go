// Command todo serves the TodoService over Connect, gRPC and REST.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/raveesh-me/dedo/examples/todo/app"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	enforce := flag.Bool("enforce-scopes", true, "reject calls without a declared scope; false only logs them")
	access := flag.String("mcp-access", "public", "who may reach /mcp: public, or authenticated (callers with a known token)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler, err := app.NewHandler(app.Config{Log: log, EnforceScopes: *enforce, MCPAccess: mcpserver.Access(*access)})
	if err != nil {
		log.Error("startup failed", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", *addr)
	if err := server.ListenAndServe(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
