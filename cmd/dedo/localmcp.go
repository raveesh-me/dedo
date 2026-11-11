package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"

	"gopkg.in/yaml.v3"
)

const (
	// mcpRemote is the stdio-to-HTTP bridge Claude Desktop launches, pinned:
	// npx would otherwise run whatever release is newest that day.
	mcpRemote = "mcp-remote@0.15.2"
	// nodeImage runs mcpRemote, so the host needs no Node.
	nodeImage = "docker.io/library/node:24-alpine"
	// kindConfig maps the cluster's NodePort to a host port.
	kindConfig = "zarf/k8s/dev/kind-config.yaml"
	// kindNodePort is the NodePort the dev overlay serves the API on.
	kindNodePort = 30080
	// runPort is where make run serves, for a repository without kind.
	runPort = 8080
)

// gatewayHosts are the names a container of each runtime reaches its
// host's loopback by.
var gatewayHosts = map[string]string{"podman": "host.containers.internal", "docker": "host.docker.internal"}

// localMCP is what dedo local-mcp-claude adds to Claude Desktop.
type localMCP struct {
	name, url, token, runtime string
}

// tokenChars is what a bearer token may hold (RFC 6750 b64token), so a
// token can never add a header or argument of its own.
var tokenChars = regexp.MustCompile(`^[A-Za-z0-9\-._~+/]+=*$`)

// serverName is what a Claude Desktop server may be called here.
var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// entry is the mcpServers entry: Claude Desktop launches the runtime, which
// runs mcp-remote in a throwaway container bridging stdio to the service's
// /mcp over HTTP. Nothing is kept between calls: the service answers each
// one statelessly, from its own state.
func (l localMCP) entry() (json.RawMessage, error) {
	u, err := url.Parse(l.url)
	if err != nil {
		return nil, fmt.Errorf("-url: %w", err)
	}
	if isLoopback(u.Hostname()) {
		port := u.Port()
		u.Host = gatewayHosts[l.runtime]
		if port != "" {
			u.Host = net.JoinHostPort(u.Host, port)
		}
	}
	args := []string{"run", "-i", "--rm"}
	if l.runtime == "docker" {
		// Docker on Linux has no host.docker.internal unless asked for.
		args = append(args, "--add-host=host.docker.internal:host-gateway")
	}
	args = append(args, nodeImage, "npx", "-y", mcpRemote, u.String(),
		"--header", "Authorization: Bearer "+l.token)
	if u.Scheme == "http" {
		// mcp-remote takes plain HTTP only for localhost, which the
		// container's name for the host is not.
		args = append(args, "--allow-http")
	}
	return json.Marshal(struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{l.runtime, args})
}

func (l localMCP) validate() error {
	if !serverName.MatchString(l.name) {
		return fmt.Errorf("-name %q: use letters, digits, '.', '_' and '-'", l.name)
	}
	if !tokenChars.MatchString(l.token) {
		return errors.New("-token: not a bearer token (letters, digits and -._~+/ only)")
	}
	if _, ok := gatewayHosts[l.runtime]; !ok {
		return fmt.Errorf("-runtime %q: want podman or docker", l.runtime)
	}
	u, err := url.Parse(l.url)
	switch {
	case err != nil:
		return fmt.Errorf("-url: %w", err)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("-url %q: want an http or https URL", l.url)
	case u.Host == "":
		return fmt.Errorf("-url %q: no host", l.url)
	case u.User != nil:
		return fmt.Errorf("-url %q: put credentials in -token, not the URL", l.url)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// localMCPClaude adds the service's MCP endpoint to Claude Desktop's config,
// or removes it, or prints the entry.
func localMCPClaude(args []string, out io.Writer) error {
	l, cfg, print, remove, err := localMCPFlags(args, out)
	if err != nil {
		return err
	}
	if remove {
		if !serverName.MatchString(l.name) {
			return fmt.Errorf("-name %q: not a server dedo local-mcp-claude adds", l.name)
		}
		return removeMCPServer(cfg, l.name, out)
	}
	if err := l.validate(); err != nil {
		return err
	}
	e, err := l.entry()
	if err != nil {
		return err
	}
	if print {
		b, err := object{{l.name, e}}.indent()
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	}
	if err := setMCPServer(cfg, l.name, e); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: mcpServers.%s bridges %s with %s in %s\n", cfg, l.name, l.url, mcpRemote, l.runtime)
	fmt.Fprintln(out, "restart Claude Desktop to load it")
	if runtime.GOOS == "darwin" {
		fmt.Fprintf(out, "if it shows as failed, its log is ~/Library/Logs/Claude/mcp-server-%s.log\n", l.name)
	}
	return nil
}

func localMCPFlags(args []string, out io.Writer) (l localMCP, cfg string, print, remove bool, err error) {
	flags := flag.NewFlagSet("local-mcp-claude", flag.ContinueOnError)
	flags.SetOutput(out)
	dir := flags.String("dir", ".", "the service repository: its go.mod names the server, its "+kindConfig+" gives the port")
	name := flags.String("name", "", "the server's name in Claude Desktop (default: the Go module's last element)")
	rawURL := flags.String("url", "", "the MCP endpoint (default: http://localhost:<kind hostPort>/mcp, or :8080 without kind)")
	token := flags.String("token", "reader", "the bearer token the bridge sends")
	rt := flags.String("runtime", defaultRuntime(), "the container runtime Claude Desktop launches: podman or docker")
	config := flags.String("config", "", "Claude Desktop's config file (default: claude_desktop_config.json in its folder)")
	printOnly := flags.Bool("print", false, "print the entry instead of writing it")
	rm := flags.Bool("remove", false, "remove the server from Claude Desktop instead")
	if err = flags.Parse(args); err != nil {
		return
	}
	if flags.NArg() > 0 {
		err = fmt.Errorf("unexpected argument %q", flags.Arg(0))
		return
	}
	l = localMCP{name: *name, url: *rawURL, token: *token, runtime: *rt}
	if l.name == "" {
		if l.name, err = moduleName(*dir); err != nil {
			return
		}
	}
	if l.url == "" {
		port, found, perr := kindHostPort(*dir)
		if perr != nil {
			err = perr
			return
		}
		if !found {
			fmt.Fprintf(out, "no %s: using make run's port %d\n", kindConfig, runPort)
		}
		l.url = "http://localhost:" + strconv.Itoa(port) + "/mcp"
	}
	cfg = *config
	if cfg == "" && !*printOnly {
		cfg, err = claudeDesktopConfig()
	}
	return l, cfg, *printOnly, *rm, err
}

// defaultRuntime is podman if it is installed, as dedo's guidance asks,
// else docker if that is.
func defaultRuntime() string {
	if _, err := exec.LookPath("podman"); err != nil {
		if _, err := exec.LookPath("docker"); err == nil {
			return "docker"
		}
	}
	return "podman"
}

// moduleName names the service of dir's go.mod: serviceName of its module.
func moduleName(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("naming the server from go.mod: %w; or give -name", err)
	}
	m := moduleLine.FindSubmatch(b)
	if m == nil {
		return "", errors.New("go.mod names no module; give -name")
	}
	return serviceName(string(m[1])), nil
}

// serviceName is a module path's last element, skipping a major-version
// suffix: example.com/todo/v2 is todo.
func serviceName(module string) string {
	name := path.Base(module)
	if dir := path.Dir(module); majorVersion.MatchString(name) && dir != "." {
		name = path.Base(dir)
	}
	return name
}

// kindHostPort is the host port dir's kind config maps the dev overlay's
// NodePort to, or make run's port when there is no kind config.
func kindHostPort(dir string) (int, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, kindConfig))
	if errors.Is(err, fs.ErrNotExist) {
		return runPort, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var c struct {
		Nodes []struct {
			ExtraPortMappings []struct {
				ContainerPort int `yaml:"containerPort"`
				HostPort      int `yaml:"hostPort"`
			} `yaml:"extraPortMappings"`
		} `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return 0, false, fmt.Errorf("%s: %w", kindConfig, err)
	}
	for _, n := range c.Nodes {
		for _, m := range n.ExtraPortMappings {
			if m.ContainerPort == kindNodePort && m.HostPort > 0 {
				return m.HostPort, true, nil
			}
		}
	}
	return 0, false, fmt.Errorf("%s maps no hostPort to containerPort %d; give -url", kindConfig, kindNodePort)
}

// claudeDesktopConfig is where Claude Desktop keeps its config: in
// ~/Library/Application Support/Claude on macOS, %APPDATA%\Claude on
// Windows and ~/.config/Claude on Linux.
func claudeDesktopConfig() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding Claude Desktop's config: %w; give -config", err)
	}
	return filepath.Join(dir, "Claude", "claude_desktop_config.json"), nil
}
