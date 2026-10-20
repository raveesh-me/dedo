package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/structpb"
)

const echoProcedure = "/dedo.test.v1.EchoService/Echo"

// echoBackend answers with the caller's token and the request value, or
// fails with a declared reason for "missing".
func echoBackend() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(echoProcedure, connect.NewUnaryHandler(echoProcedure,
		func(_ context.Context, req *connect.Request[structpb.Struct]) (*connect.Response[structpb.Struct], error) {
			say := req.Msg.GetFields()["say"].GetStringValue()
			if say == "missing" {
				err := connect.NewError(connect.CodeNotFound, errors.New("no such thing"))
				if d, derr := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: "THING_NOT_FOUND"}); derr == nil {
					err.AddDetail(d)
				}
				return nil, err
			}
			out, err := structpb.NewStruct(map[string]any{"auth": req.Header().Get("Authorization"), "said": say})
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(out), nil
		}))
	return mux
}

// echoTool is what protoc-gen-dedo-mcp generates for an Echo RPC.
func echoTool() Tool {
	return Tool{
		Tool: &mcp.Tool{
			Name:         "echo",
			Title:        "Echo",
			Description:  "Echoes.",
			Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: Bool(false)},
			InputSchema:  json.RawMessage(`{"type":"object","properties":{"say":{"type":"string"}}}`),
			OutputSchema: json.RawMessage(`{"type":"object"}`),
		},
		Call: Unary[structpb.Struct, structpb.Struct](echoProcedure),
	}
}

func typedSession(t *testing.T, tools ...Tool) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	if err := AddTools(server, Target{Client: InProcessClient(echoBackend())}, tools...); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	t.Cleanup(ts.Close)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v1"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: &http.Client{Transport: bearer{token: "t"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// A generated tool is listed as written and forwards with the caller's
// token, using the typed messages.
func TestAddToolsForwards(t *testing.T) {
	cs := typedSession(t, echoTool())
	ctx := context.Background()
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Title != "Echo" || !list.Tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("tools = %+v, %v", list, err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"say": "hi"}})
	if err != nil || res.IsError {
		t.Fatalf("echo: %v %s", err, text(res))
	}
	if got, _ := json.Marshal(res.StructuredContent); string(got) != `{"auth":"Bearer t","said":"hi"}` {
		t.Errorf("structuredContent = %s", got)
	}

	failed, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"say": "missing"}})
	if err != nil || !failed.IsError {
		t.Fatalf("missing: %v, isError %v", err, failed.IsError)
	}
	var te ToolError
	if err := json.Unmarshal([]byte(text(failed)), &te); err != nil || te.Code != "NOT_FOUND" || te.Reason != "THING_NOT_FOUND" {
		t.Errorf("tool error = %s", text(failed))
	}
}

func TestAddToolsRejects(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	client := Target{Client: InProcessClient(echoBackend())}
	bad := echoTool()
	bad.Tool = &mcp.Tool{Name: "Not Valid", InputSchema: json.RawMessage(`{"type":"object"}`)}
	for name, tc := range map[string]struct {
		target Target
		tools  []Tool
		want   string
	}{
		"no client":    {Target{}, []Tool{echoTool()}, "Client"},
		"no call":      {client, []Tool{{Tool: echoTool().Tool}}, "lacks"},
		"invalid name": {client, []Tool{bad}, "not a valid"},
		"duplicate":    {client, []Tool{echoTool(), echoTool()}, "two tools"},
	} {
		if err := AddTools(server, tc.target, tc.tools...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

// Describe is what the generator writes out: the same tool New serves.
func TestDescribeMatchesNew(t *testing.T) {
	m := service(t).Methods().ByName("DeleteThing")
	got, err := Describe(m)
	if err != nil {
		t.Fatal(err)
	}
	res, err := session(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, served := range res.Tools {
		if served.Name != got.Name {
			continue
		}
		if served.Title != got.Title || served.Description != got.Description ||
			*served.Annotations.DestructiveHint != *got.Annotations.DestructiveHint {
			t.Errorf("Describe = %+v, New serves %+v", got, served)
		}
		return
	}
	t.Errorf("New serves no tool %q", got.Name)
}
