package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeConfig is a Claude Desktop config with members of its own.
const claudeConfig = `{
  "preferences": {"quickEntryShortcut": "off"},
  "mcpServers": {
    "other": {"command": "other-server"}
  }
}
`

func runLocalMCP(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(append([]string{"local-mcp-claude"}, args...), &out)
	return out.String(), err
}

type desktopServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func serversIn(t *testing.T, cfg string) (map[string]desktopServer, []string) {
	t.Helper()
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parseObject(b)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range doc {
		order = append(order, m.Name)
	}
	var v struct {
		MCPServers map[string]desktopServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.MCPServers, order
}

func TestLocalMCPClaudeAddsTheKindCluster(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":      "module github.com/acme/time-counter\n\ngo 1.26\n",
		kindConfig:    "kind: Cluster\nnodes:\n- role: control-plane\n  extraPortMappings:\n  - containerPort: 30080\n    hostPort: 8091\n",
		"claude.json": claudeConfig,
	})
	cfg := filepath.Join(dir, "claude.json")
	if _, err := runLocalMCP(t, "-dir", dir, "-config", cfg, "-runtime", "podman", "-token", "writer"); err != nil {
		t.Fatal(err)
	}
	servers, order := serversIn(t, cfg)
	want := desktopServer{Command: "podman", Args: []string{
		"run", "-i", "--rm", nodeImage, "npx", "-y", mcpRemote,
		"http://host.containers.internal:8091/mcp",
		"--header", "Authorization: Bearer writer", "--allow-http",
	}}
	if got := servers["time-counter"]; got.Command != want.Command || strings.Join(got.Args, " ") != strings.Join(want.Args, " ") {
		t.Errorf("time-counter = %+v\nwant %+v", got, want)
	}
	if servers["other"].Command != "other-server" {
		t.Error("another server was lost")
	}
	if strings.Join(order, ",") != "preferences,mcpServers" {
		t.Errorf("members reordered: %v", order)
	}
	if b, err := os.ReadFile(cfg + ".bak"); err != nil || string(b) != claudeConfig {
		t.Errorf("no backup of the previous config: %v", err)
	}

	// Again: nothing to change, so the file is not rewritten.
	before, _ := os.ReadFile(cfg)
	if err := os.Remove(cfg + ".bak"); err != nil {
		t.Fatal(err)
	}
	if _, err := runLocalMCP(t, "-dir", dir, "-config", cfg, "-runtime", "podman", "-token", "writer"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(cfg); !bytes.Equal(before, after) {
		t.Error("a second run changed the config")
	}
	if _, err := os.Stat(cfg + ".bak"); err == nil {
		t.Error("a second run wrote a backup")
	}

	// Remove it; the other server stays.
	if _, err := runLocalMCP(t, "-dir", dir, "-config", cfg, "-remove"); err != nil {
		t.Fatal(err)
	}
	servers, _ = serversIn(t, cfg)
	if _, ok := servers["time-counter"]; ok || servers["other"].Command != "other-server" {
		t.Errorf("after -remove: %+v", servers)
	}
}

func TestLocalMCPClaudeCreatesTheConfig(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/shop\n"})
	cfg := filepath.Join(dir, "Claude", "claude_desktop_config.json")
	out, err := runLocalMCP(t, "-dir", dir, "-config", cfg, "-runtime", "docker")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "make run's port 8080") {
		t.Errorf("output does not say it fell back to make run:\n%s", out)
	}
	servers, _ := serversIn(t, cfg)
	got := strings.Join(servers["shop"].Args, " ")
	for _, want := range []string{"--add-host=host.docker.internal:host-gateway", "http://host.docker.internal:8080/mcp", "Authorization: Bearer reader"} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q: %s", want, got)
		}
	}
	if info, err := os.Stat(cfg); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("new config mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestLocalMCPClaudePrintsAndKeepsRemoteHosts(t *testing.T) {
	out, err := runLocalMCP(t, "-name", "shop", "-url", "https://shop.example/mcp", "-runtime", "podman", "-print")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"https://shop.example/mcp"`) || strings.Contains(out, "--allow-http") {
		t.Errorf("a remote HTTPS URL was changed:\n%s", out)
	}
	if !strings.HasPrefix(out, "{\n  \"shop\": {") {
		t.Errorf("not an mcpServers entry:\n%s", out)
	}
}

func TestLocalMCPClaudeRejects(t *testing.T) {
	tests := map[string][]string{
		"token with a space":  {"-name", "x", "-url", "http://localhost:1/mcp", "-token", "a b", "-print"},
		"token with newline":  {"-name", "x", "-url", "http://localhost:1/mcp", "-token", "a\nX-Evil: 1", "-print"},
		"unknown runtime":     {"-name", "x", "-url", "http://localhost:1/mcp", "-runtime", "lxc", "-print"},
		"not http":            {"-name", "x", "-url", "file:///etc/passwd", "-print"},
		"credentials in URL":  {"-name", "x", "-url", "http://u:p@localhost:1/mcp", "-print"},
		"bad name":            {"-name", "x y", "-url", "http://localhost:1/mcp", "-print"},
		"no go.mod, no -name": {"-dir", t.TempDir(), "-url", "http://localhost:1/mcp", "-print"},
		"stray argument":      {"-name", "x", "-print", "extra"},
	}
	for name, args := range tests {
		if _, err := runLocalMCP(t, args...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLocalMCPClaudeRefusesABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runLocalMCP(t, "-name", "x", "-url", "http://localhost:1/mcp", "-config", cfg, "-runtime", "podman"); err == nil {
		t.Fatal("no error for a config that is not JSON")
	}
	if b, _ := os.ReadFile(cfg); string(b) != `{"mcpServers": [` {
		t.Errorf("the broken config was overwritten: %q", b)
	}
}

func TestLocalMCPClaudeRemoveWithoutAConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "claude.json")
	out, err := runLocalMCP(t, "-name", "x", "-config", cfg, "-remove")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("-remove created a config")
	}
	if !strings.Contains(out, "nothing to remove") {
		t.Errorf("output = %q", out)
	}
}

func TestLocalMCPClaudeRefusesDuplicateKeys(t *testing.T) {
	for name, cfgText := range map[string]string{
		"mcpServers twice": `{"mcpServers": {}, "mcpServers": {"x": {"command": "y"}}}`,
		"server twice":     `{"mcpServers": {"x": {"command": "a"}, "x": {"command": "b"}}}`,
	} {
		cfg := filepath.Join(t.TempDir(), "claude.json")
		if err := os.WriteFile(cfg, []byte(cfgText), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := runLocalMCP(t, "-name", "x", "-url", "http://localhost:1/mcp", "-config", cfg, "-runtime", "podman"); err == nil {
			t.Errorf("%s: no error", name)
		}
		if b, _ := os.ReadFile(cfg); string(b) != cfgText {
			t.Errorf("%s: the config was changed", name)
		}
	}
}

func TestLocalMCPClaudeFollowsASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "claude.json")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(claudeConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "claude_desktop_config.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := runLocalMCP(t, "-name", "x", "-url", "http://localhost:1/mcp", "-config", link, "-runtime", "podman"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	servers, _ := serversIn(t, real)
	if _, ok := servers["x"]; !ok {
		t.Error("the link's target lacks the server")
	}
	if info, err := os.Stat(real + ".bak"); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("backup beside the target: %v, want mode 0600", err)
	}
}

func TestServiceName(t *testing.T) {
	for module, want := range map[string]string{
		"example.com/todo":    "todo",
		"example.com/todo/v2": "todo",
		"todo":                "todo",
		"v2":                  "v2",
		"example.com/v2x":     "v2x",
	} {
		if got := serviceName(module); got != want {
			t.Errorf("serviceName(%q) = %q, want %q", module, got, want)
		}
	}
}
