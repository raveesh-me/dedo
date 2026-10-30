package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

// headerBackend answers with the request headers the service received.
func headerBackend() *connect.Client {
	server := connect.NewServer()
	server.Register(connect.Method{
		Spec: connect.Spec{Procedure: echoProcedure, StreamType: connect.StreamTypeUnary},
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(new(structpb.Struct)); err != nil {
				return err
			}
			info, _ := connect.CallInfoForServerContext(ctx)
			got := map[string]any{}
			for _, name := range []string{"Authorization", "X-Flow-ID", "Version", "X-Other"} {
				got[name] = info.RequestHeader().Get(name)
			}
			out, err := structpb.NewStruct(got)
			if err != nil {
				return err
			}
			return stream.Send(out)
		},
	})
	return connect.NewClient(connectinprocess.New(server))
}

// headers sets fixed headers on every request of the MCP client.
type headers map[string]string

func (h headers) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range h {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// The service gets Authorization and X-Flow-ID always, the configured
// ForwardHeaders when the caller sent them, and nothing else.
func TestForwardHeaders(t *testing.T) {
	sent := headers{"Authorization": "Bearer t", "X-Flow-ID": "flow-1", "Version": "2021-07-28", "X-Other": "no"}
	for name, tc := range map[string]struct {
		forward []string
		sent    headers
		want    map[string]string
	}{
		"defaults only": {nil, sent,
			map[string]string{"Authorization": "Bearer t", "X-Flow-ID": "flow-1", "Version": "", "X-Other": ""}},
		"configured": {[]string{"version"}, sent,
			map[string]string{"Authorization": "Bearer t", "X-Flow-ID": "flow-1", "Version": "2021-07-28", "X-Other": ""}},
		"not sent": {[]string{"Version"}, headers{"Authorization": "Bearer t"},
			map[string]string{"Authorization": "Bearer t", "X-Flow-ID": "", "Version": "", "X-Other": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
			if err := AddToolsWith(server, Options{Client: headerBackend(), ForwardHeaders: tc.forward}, echoTool()); err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
				&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
			t.Cleanup(ts.Close)
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v1"}, nil).Connect(context.Background(),
				&mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: &http.Client{Transport: tc.sent}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cs.Close() })
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{}})
			if err != nil || res.IsError {
				t.Fatalf("echo: %v %s", err, text(res))
			}
			raw, _ := json.Marshal(res.StructuredContent)
			var got map[string]string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestForwardHeadersRejectsInvalidNames(t *testing.T) {
	for _, bad := range [][]string{{""}, {"Not Valid"}, {"Version", "X:Y"}} {
		server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
		err := AddToolsWith(server, Options{Client: headerBackend(), ForwardHeaders: bad}, echoTool())
		if err == nil || !strings.Contains(err.Error(), "not a valid header name") {
			t.Errorf("AddToolsWith(%q): err = %v", bad, err)
		}
		svc := service(t)
		err = New(server, Config{
			Services: []protoreflect.ServiceDescriptor{svc}, InputSchemas: inputSchemas, OutputSchemas: outputSchemas,
			Client: backend(svc), ForwardHeaders: bad,
		})
		if err == nil || !strings.Contains(err.Error(), "not a valid header name") {
			t.Errorf("New(%q): err = %v", bad, err)
		}
	}
}
