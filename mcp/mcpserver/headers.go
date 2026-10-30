package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// forwardedHeaders are always copied from the MCP request onto the forwarded
// call: the caller's token and flow ID.
var forwardedHeaders = []string{"Authorization", "X-Flow-ID"}

// extraHeadersKey carries a server's ForwardHeaders to forwardHeaders.
type extraHeadersKey struct{}

// forwardHeaders puts the MCP caller's token and flow ID, and the headers
// withHeaders added, on the forwarded call.
func forwardHeaders(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req == nil || req.Extra == nil {
		return ctx
	}
	extra, _ := ctx.Value(extraHeadersKey{}).([]string)
	ctx, info := connect.NewClientContext(ctx)
	for _, names := range [][]string{forwardedHeaders, extra} {
		for _, name := range names {
			if v := req.Extra.Header.Get(name); v != "" {
				info.RequestHeader().Set(name, v)
			}
		}
	}
	return ctx
}

// withHeaders makes a tool handler forward names on top of
// forwardedHeaders.
func withHeaders(h mcp.ToolHandlerFor[json.RawMessage, json.RawMessage], names []string) mcp.ToolHandlerFor[json.RawMessage, json.RawMessage] {
	if len(names) == 0 {
		return h
	}
	names = append([]string(nil), names...)
	return func(ctx context.Context, req *mcp.CallToolRequest, args json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
		return h(context.WithValue(ctx, extraHeadersKey{}, names), req, args)
	}
}

// checkHeaders fails on a name that is not an HTTP header field name.
func checkHeaders(names []string) error {
	for _, name := range names {
		if name == "" || strings.ContainsFunc(name, func(r rune) bool { return !isTokenRune(r) }) {
			return fmt.Errorf("mcpserver: forwarded header %q is not a valid header name", name)
		}
	}
	return nil
}

// isTokenRune reports whether r may appear in an HTTP token (RFC 9110).
func isTokenRune(r rune) bool {
	return r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		strings.ContainsRune("!#$%&'*+-.^_`|~", r))
}
