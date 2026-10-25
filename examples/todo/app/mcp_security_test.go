package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/examples/todo/app"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
)

// These tests pin what the MCP path must keep doing, through the real
// /mcp handler with the token in the Authorization header, as production
// sends it. Cases covered elsewhere:
//
//   - a reader calling a write tool is denied, and scope enforcement lets
//     a writer through: TestMCPCallsEnforceScopes, and the MCP part of
//     TestEverySurfaceRunsTheInterceptors;
//   - a cross-site Origin is rejected: TestMCPRejectsCrossOrigin;
//   - an invalid email is rejected: TestValidationRules.

// mcpCall posts one tools/call to /mcp and returns the response and the
// JSON-RPC result. An empty token sends no Authorization header.
func mcpCall(t *testing.T, url, token, flowID, tool, args string) (*http.Response, mcpResult) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if flowID != "" {
		req.Header.Set(flowid.Header, flowID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result mcpResult `json:"result"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
	}
	return res, out.Result
}

type mcpResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (r mcpResult) text() string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// A tool call without a token, or with one the service does not know, is
// denied by the scope check, before the handler runs.
func TestMCPUnauthenticatedCallsDenied(t *testing.T) {
	e := newEnv(t)
	for _, token := range []string{"", "nobody"} {
		res, got := mcpCall(t, e.url, token, "", getTool, `{"name":"todos/x"}`)
		if res.StatusCode != http.StatusOK || !got.IsError || !strings.Contains(got.text(), `"code":"PERMISSION_DENIED"`) {
			t.Errorf("token %q: %d isError=%v %s", token, res.StatusCode, got.IsError, got.text())
		}
	}
	// A known token passes the check and reaches the handler.
	if _, got := mcpCall(t, e.url, app.ReaderToken, "", getTool, `{"name":"todos/x"}`); !strings.Contains(got.text(), "TODO_NOT_FOUND") {
		t.Errorf("reader: %s", got.text())
	}
}

// Malformed arguments get the same results as today: the input schema
// rejects wrong types, missing fields and non-object arguments before the
// call, and fields the schema does not name are dropped.
func TestMCPMalformedArguments(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name, args, want string
	}{
		{"wrong type", `{"name":7}`, `validating /properties/name: type: 7 has type "integer", want "string"`},
		{"missing field", `{}`, `required: missing properties: ["name"]`},
		{"array", `[1]`, `cannot unmarshal "[1]" into Go value of type map[string]interface {}`},
		{"string", `"x"`, `cannot unmarshal "\"x\"" into Go value of type map[string]interface {}`},
		// The unknown field is dropped and the call goes through.
		{"unknown field", `{"name":"todos/x","bogus":1}`, `"reason":"TODO_NOT_FOUND"`},
	}
	for _, tt := range tests {
		res, got := mcpCall(t, e.url, app.ReaderToken, "", getTool, tt.args)
		if res.StatusCode != http.StatusOK || !got.IsError || !strings.Contains(got.text(), tt.want) {
			t.Errorf("%s: %d isError=%v %s, want %s", tt.name, res.StatusCode, got.IsError, got.text(), tt.want)
		}
	}
}

// An MCP call keeps the caller's flow ID: the response carries it, and so
// do the logs of the error audit and of the audit-mode scope check.
func TestMCPFlowIDAndAudit(t *testing.T) {
	const flow = "mcp-flow-1"
	badBudget := `{"todo":{"title":"x","budget_amount":{"currency_code":"EUR","units":"1","nanos":-5}}}`

	e := newEnv(t)
	res, got := mcpCall(t, e.url, app.WriterToken, flow, createTool, badBudget)
	if res.Header.Get(flowid.Header) != flow {
		t.Errorf("response flow ID %q, want %q", res.Header.Get(flowid.Header), flow)
	}
	if !strings.Contains(got.text(), "INVALID_ARGUMENT") {
		t.Errorf("create with a bad budget: %s", got.text())
	}
	// buf.validate's error carries no reason, so the audit logs it.
	if log := e.log.String(); !strings.Contains(log, `msg="error without ErrorInfo"`) || !strings.Contains(log, "flow_id="+flow) {
		t.Errorf("error audit log: %s", log)
	}

	var log bytes.Buffer
	handler, err := app.NewHandler(app.Config{Log: slog.New(slog.NewTextHandler(&log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	audit := httptest.NewServer(handler)
	t.Cleanup(audit.Close)
	mcpCall(t, audit.URL, app.ReaderToken, flow, createTool, `{"todo":{"title":"x"}}`)
	if !strings.Contains(log.String(), `msg="scope check failed in audit mode"`) || !strings.Contains(log.String(), "flow_id="+flow) {
		t.Errorf("audit-mode log: %s", log.String())
	}
}

// With MCPAccess authenticated, only callers with a known token reach /mcp
// at all, tool discovery included; the default keeps tools/list public, as
// TestMCPRejectsCrossOrigin shows.
func TestMCPAccessAuthenticated(t *testing.T) {
	var log bytes.Buffer
	handler, err := app.NewHandler(app.Config{
		Log: slog.New(slog.NewTextHandler(&log, nil)), EnforceScopes: true, MCPAccess: mcpserver.AccessAuthenticated,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	for _, token := range []string{"", "nobody"} {
		res, _ := mcpCall(t, srv.URL, token, "", getTool, `{"name":"todos/x"}`)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get(flowid.Header) == "" {
			t.Errorf("token %q: status %d, flow ID %q; want 401 with a flow ID", token, res.StatusCode, res.Header.Get(flowid.Header))
		}
	}
	tools, err := mcpSession(t, env{url: srv.URL}, app.ReaderToken).ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("reader: tools/list: %v, %+v", err, tools)
	}
}
