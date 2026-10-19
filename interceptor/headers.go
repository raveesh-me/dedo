package interceptor

import (
	"fmt"
	"net/http"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// setHeaders sets Cache-Control on GET responses of cacheable methods, and
// Deprecation (RFC 9745) and Sunset (RFC 8594) on deprecated methods.
func setHeaders(h http.Header, httpMethod string, rules *dedov1.MethodRules) {
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

func cacheControl(c *dedov1.CachePolicy) string {
	visibility := "private"
	if c.GetVisibility() == dedov1.CachePolicy_VISIBILITY_PUBLIC {
		visibility = "public"
	}
	return fmt.Sprintf("%s, max-age=%d", visibility, int64(c.GetMaxAge().AsDuration().Seconds()))
}
