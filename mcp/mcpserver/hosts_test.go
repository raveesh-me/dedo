package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAllowedHosts(t *testing.T) {
	tests := []struct {
		env  string
		want []string
	}{
		{"", []string{"host.containers.internal", "host.docker.internal"}},
		{"   ", []string{"host.containers.internal", "host.docker.internal"}},
		{"Gateway.Local", []string{"gateway.local"}},
		{" a.internal , ,b.internal ", []string{"a.internal", "b.internal"}},
	}
	for _, tt := range tests {
		if got := AllowedHosts(tt.env); !slices.Equal(got, tt.want) {
			t.Errorf("AllowedHosts(%q) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestAllowedHostsReturnsACopy(t *testing.T) {
	AllowedHosts("")[0] = "evil.example"
	if got := AllowedHosts("")[0]; got != "host.containers.internal" {
		t.Fatalf("the defaults changed to %q", got)
	}
}

// The MCP SDK refuses a Host other than localhost on a loopback connection.
// httptest.NewServer listens on 127.0.0.1, so the check runs here as it does
// for a service on a developer's machine.
func TestAllowHostsThroughRebindingProtection(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	srv := httptest.NewServer(AllowHosts(sdk, AllowedHosts("")))
	defer srv.Close()
	port := srv.Listener.Addr().String()[strings.LastIndex(srv.Listener.Addr().String(), ":"):]

	tests := []struct {
		host string
		want int
	}{
		{"localhost" + port, http.StatusOK},
		{"host.containers.internal" + port, http.StatusOK},
		{"HOST.DOCKER.INTERNAL" + port, http.StatusOK},
		{"host.containers.internal", http.StatusOK},
		{"evil.example" + port, http.StatusForbidden},
		{"host.containers.internal.evil.example" + port, http.StatusForbidden},
	}
	for _, tt := range tests {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tt.host
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tt.want {
			t.Errorf("Host %q: status %d, want %d", tt.host, res.StatusCode, tt.want)
		}
	}
}

func TestAllowHostsLeavesTheRequestAlone(t *testing.T) {
	var seen string
	h := AllowHosts(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Host }), []string{"gateway.local"})
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local:8080/mcp", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "localhost:8080" {
		t.Errorf("handler saw Host %q, want localhost:8080", seen)
	}
	if req.Host != "gateway.local:8080" {
		t.Errorf("the caller's request changed: Host %q", req.Host)
	}
}
