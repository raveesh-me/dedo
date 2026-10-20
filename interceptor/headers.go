package interceptor

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// setDeprecation sets Deprecation (RFC 9745) and Sunset (RFC 8594) on
// deprecated methods.
func setDeprecation(h *connect.Header, rules *dedov1.MethodRules) {
	d := rules.GetDeprecation()
	if d == nil {
		return
	}
	if t := d.GetDeprecationTime(); t != nil {
		h.Set("Deprecation", fmt.Sprintf("@%d", t.AsTime().Unix()))
	}
	if t := d.GetSunsetTime(); t != nil {
		h.Set("Sunset", t.AsTime().UTC().Format(http.TimeFormat))
	}
}

// cacheable reports whether a response may carry Cache-Control: the method
// declares a cache policy and the call is an HTTP GET. A REST call through
// Vanguard counts as one, since a method with a cache policy is read-only
// and its REST binding a GET; a Connect call counts when it used GET.
func cacheable(ctx context.Context, info *connect.CallInfo, rules *dedov1.MethodRules) bool {
	if rules.GetCache() == nil {
		return false
	}
	if info.Protocol == "rest" {
		return true
	}
	httpInfo, _ := connecthttp.ServerInfoForContext(ctx)
	return httpInfo.HTTPMethod() == http.MethodGet
}

func cacheControl(c *dedov1.CachePolicy) string {
	visibility := "private"
	if c.GetVisibility() == dedov1.CachePolicy_VISIBILITY_PUBLIC {
		visibility = "public"
	}
	return fmt.Sprintf("%s, max-age=%d", visibility, int64(c.GetMaxAge().AsDuration().Seconds()))
}

// cacheStream sets Cache-Control when the handler sends its first response
// or its headers, which is before the headers go out.
type cacheStream struct {
	connect.ServerStream
	header *connect.Header
	value  string
	set    bool
}

func (s *cacheStream) setHeader() {
	if !s.set {
		s.header.Set("Cache-Control", s.value)
		s.set = true
	}
}

func (s *cacheStream) SendHeaders() error {
	s.setHeader()
	return s.ServerStream.SendHeaders()
}

func (s *cacheStream) Send(msg any) error {
	s.setHeader()
	return s.ServerStream.Send(msg)
}
