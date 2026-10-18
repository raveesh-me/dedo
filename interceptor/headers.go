package interceptor

import (
	"fmt"
	"net/http"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
)

// setHeaders sets Cache-Control on GET responses of cacheable methods, and
// Deprecation (RFC 9745) and Sunset (RFC 8594) on deprecated methods.
func setHeaders(h http.Header, httpMethod string, rules *hplv1.MethodRules) {
	if c := rules.GetCache(); c != nil && httpMethod == http.MethodGet {
		h.Set("Cache-Control", cacheControl(c))
	}
	if d := rules.GetDeprecation(); d != nil {
		if t := d.GetDeprecationTime(); t != nil {
			h.Set("Deprecation", fmt.Sprintf("@%d", t.AsTime().Unix()))
		}
		if t := d.GetSunsetTime(); t != nil {
			h.Set("Sunset", t.AsTime().UTC().Format(http.TimeFormat))
		}
	}
}

func cacheControl(c *hplv1.CachePolicy) string {
	visibility := "private"
	if c.GetVisibility() == hplv1.CachePolicy_VISIBILITY_PUBLIC {
		visibility = "public"
	}
	return fmt.Sprintf("%s, max-age=%d", visibility, int64(c.GetMaxAge().AsDuration().Seconds()))
}
