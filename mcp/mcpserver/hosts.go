package mcpserver

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

// containerHosts are the names a container uses to reach its host: Podman's
// and Docker's. A browser on the host never sends them, so allowing them
// leaves DNS rebinding protection in place for every other name.
var containerHosts = []string{"host.containers.internal", "host.docker.internal"}

// AllowedHosts returns the host names AllowHosts lets through, from a
// comma-separated list such as the MCP_ALLOWED_HOSTS variable: lower-cased,
// blanks dropped. An empty list means the container gateway names,
// host.containers.internal and host.docker.internal.
func AllowedHosts(list string) []string {
	if strings.TrimSpace(list) == "" {
		return slices.Clone(containerHosts)
	}
	var hosts []string
	for h := range strings.SplitSeq(list, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// AllowHosts serves a request whose Host names one of hosts as if it named
// localhost, and every other request unchanged.
//
// The MCP SDK's DNS rebinding protection refuses any Host but localhost on a
// loopback connection. A bridge running in a container, such as mcp-remote
// under Podman for Claude Desktop, reaches the host's loopback as
// host.containers.internal, so without this every call it makes gets 403.
// Wrap the SDK's handler, inside any Origin check, so the check still sees
// the Host the caller sent.
func AllowHosts(next http.Handler, hosts []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			name, port = r.Host, ""
		}
		if !slices.Contains(hosts, strings.ToLower(name)) {
			next.ServeHTTP(w, r)
			return
		}
		local := r.Clone(r.Context())
		local.Host = "localhost"
		if port != "" {
			local.Host = net.JoinHostPort("localhost", port)
		}
		next.ServeHTTP(w, local)
	})
}
