package app

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"connectrpc.com/connect/v2"
)

// Demo bearer tokens, the scopes they grant and the locations (tenants) they
// may act in. A real service reads both from a verified token instead.
const (
	// Loc1Reader reads contacts and locations in locations/1.
	Loc1Reader = "loc1-reader"
	// Loc1Writer reads and writes contacts in locations/1, and reads it.
	Loc1Writer = "loc1-writer"
	// Loc2Writer reads and writes contacts in locations/2, and reads it.
	Loc2Writer = "loc2-writer"
	// AllReader reads contacts and locations in every location, the
	// tenant "-", and writes nothing.
	AllReader = "all-reader"
)

// allTenants grants every tenant, including AIP-159's "-" wildcard that a
// read across locations, such as locations/-/contacts, names.
const allTenants = "-"

var (
	readScopes  = []string{"contacts.contact.read", "contacts.location.read"}
	writeScopes = []string{"contacts.contact.read", "contacts.contact.write", "contacts.location.read"}
)

// grant is what a token allows: the scopes it holds, and the tenants it may
// use them in.
type grant struct {
	scopes  []string
	tenants []string
}

var tokenGrants = map[string]grant{
	Loc1Reader: {scopes: readScopes, tenants: []string{"1"}},
	Loc1Writer: {scopes: writeScopes, tenants: []string{"1"}},
	Loc2Writer: {scopes: writeScopes, tenants: []string{"2"}},
	AllReader:  {scopes: readScopes, tenants: []string{allTenants}},
}

type grantKey struct{}

// Auth stores the scopes and tenants of the caller's bearer token in the
// call's context, for the policy interceptor that runs after it. As a
// server interceptor it covers every transport that reaches the server:
// Connect, gRPC, REST and MCP tool calls. An unknown or missing token grants
// no scopes and no tenants.
func Auth() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			var token string
			if info, ok := connect.CallInfoForServerContext(ctx); ok {
				token, _ = strings.CutPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
			}
			return next(context.WithValue(ctx, grantKey{}, tokenGrants[token]), spec, stream)
		}
	}
}

// Authenticated reports whether a request carries a known token, for
// Config.MCPAccess authenticated.
func Authenticated(r *http.Request) bool {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	_, ok := tokenGrants[token]
	return ok
}

// ScopesFromContext returns the scopes stored by Auth.
func ScopesFromContext(ctx context.Context) []string {
	g, _ := ctx.Value(grantKey{}).(grant)
	return g.scopes
}

// TenantsFromContext returns the tenants stored by Auth, such as "1" for
// locations/1, or "-" for every location.
func TenantsFromContext(ctx context.Context) []string {
	g, _ := ctx.Value(grantKey{}).(grant)
	return g.tenants
}

// TenantAccess reports whether the caller may act in a tenant; it is the
// interceptor's tenant check, and the handlers use it to filter reads that
// span tenants. A caller granted "-" may act in every tenant, the "-" of a
// read across tenants included; any other caller only in the tenants it was
// granted, so never in "-".
func TenantAccess(ctx context.Context, tenant string) bool {
	tenants := TenantsFromContext(ctx)
	return slices.Contains(tenants, allTenants) || slices.Contains(tenants, tenant)
}
