package mcpserver

import (
	"fmt"
	"net/http"
)

// Access says who may reach a service's MCP endpoint. Tool calls go through
// the service's interceptors, so a caller without the scope a tool needs is
// denied whichever it is; Access decides who may discover the tools.
type Access string

const (
	// AccessPublic lets anyone list the tools and their schemas. It is the
	// default, the zero value.
	AccessPublic Access = "public"
	// AccessAuthenticated answers only requests whose credentials the
	// service accepts, tool discovery included; others get 401 before any
	// MCP handling, as the MCP authorization specification describes.
	AccessAuthenticated Access = "authenticated"
)

// Guard returns h serving MCP requests as access says. authenticated
// reports whether a request carries credentials the service accepts; it is
// needed for AccessAuthenticated only. It fails on an access it does not
// know, so a mistyped setting stops the service rather than opening it.
func Guard(h http.Handler, access Access, authenticated func(*http.Request) bool) (http.Handler, error) {
	switch access {
	case "", AccessPublic:
		return h, nil
	case AccessAuthenticated:
		if authenticated == nil {
			return nil, fmt.Errorf("mcpserver: access %q needs a way to authenticate requests", access)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !authenticated(r) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "missing or invalid credentials", http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, r)
		}), nil
	}
	return nil, fmt.Errorf("mcpserver: unknown access %q; want %q or %q", access, AccessPublic, AccessAuthenticated)
}
