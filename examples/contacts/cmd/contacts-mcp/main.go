// Command contacts-mcp serves the contacts MCP tools alone, forwarding each
// tool call over Connect to a contacts server elsewhere, such as a pod beside
// it. It is the networked counterpart of the in-process /mcp that the contacts
// command serves, for comparing the two.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/raveesh-me/dedo/examples/contacts/app"
	"github.com/raveesh-me/dedo/flowid"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	backend := flag.String("backend", "http://localhost:8081", "base URL of the contacts server the tools call")
	conns := flag.Int("conns", 256, "idle connections to keep to the backend")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	// Keep a connection per concurrent call: with http.DefaultClient's two
	// idle connections per host, a busy server opens and closes connections
	// until it runs out of local ports.
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: *conns}}
	tools, err := app.NewMCP(connect.NewClient(connecthttp.NewTransport(client, *backend)))
	if err != nil {
		log.Error("startup failed", "error", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", flowid.Middleware(tools))
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", *addr, "backend", *backend)
	if err := server.ListenAndServe(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
