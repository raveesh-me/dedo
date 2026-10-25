package app

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"

	"github.com/raveesh-me/dedo/flowid"
)

// BenchmarkMCPHop measures one multi-tenant tool call through the real /mcp handler, with
// scopes enforced and a valid token, so every measured call passes the
// security checks. Only the transport between the MCP tools and the server
// differs: in-process, or a Connect call over loopback HTTP to a second
// server. Loopback omits TLS, load balancers and a real network, so the
// difference between the two is a floor.
func BenchmarkMCPHop(b *testing.B) {
	hops := map[string]func(*testing.B, *connect.Server) *connect.Client{
		"inprocess": func(_ *testing.B, server *connect.Server) *connect.Client {
			return connect.NewClient(connectinprocess.New(server))
		},
		"loopback": func(b *testing.B, server *connect.Server) *connect.Client {
			mux := http.NewServeMux()
			connecthttp.Mount(mux, server)
			backend := httptest.NewServer(mux)
			b.Cleanup(backend.Close)
			return connect.NewClient(connecthttp.NewTransport(backend.Client(), backend.URL))
		},
	}
	// Each call is checked against the caller's location: the name, and in
	// update_ref the nested home_location reference too.
	calls := []struct{ name, tool, args string }{
		{"get", "get_contact", `{"name":"locations/1/contacts/bench"}`},
		{"update_ref", "update_contact", `{"contact":{"name":"locations/1/contacts/bench","home_location":"locations/1"},"update_mask":"home_location"}`},
	}
	for _, hop := range []string{"inprocess", "loopback"} {
		for _, call := range calls {
			b.Run(hop+"/"+call.name, func(b *testing.B) {
				server, err := newServer(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), EnforceScopes: true})
				if err != nil {
					b.Fatal(err)
				}
				tools, err := NewMCP(hops[hop](b, server))
				if err != nil {
					b.Fatal(err)
				}
				front := httptest.NewServer(flowid.Middleware(tools))
				b.Cleanup(front.Close)
				post := mcpPoster(b, front)
				for i := range 10 {
					id := "bench"
					if i > 0 {
						id = fmt.Sprintf("bench-%d", i)
					}
					post("create_contact", `{"parent":"locations/1","contact_id":"`+id+`","contact":{"display_name":"seed"}}`)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					post(call.tool, call.args)
				}
			})
		}
	}
}

// mcpPoster returns a function that posts one tools/call to the server's
// /mcp as the writer of locations/1, and fails the benchmark on any error, including a
// tool error.
func mcpPoster(b *testing.B, front *httptest.Server) func(tool, args string) {
	client := front.Client()
	return func(tool, args string) {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
		req, err := http.NewRequest(http.MethodPost, front.URL, strings.NewReader(body))
		if err != nil {
			b.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+Loc1Writer)
		res, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			b.Fatal(err)
		}
		var out struct {
			Result *struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || out.Result == nil || out.Result.IsError {
			b.Fatalf("%s: %d %s", tool, res.StatusCode, raw)
		}
	}
}
