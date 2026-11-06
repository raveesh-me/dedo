// Package bfftools (directory bff-tools) serves a backend for frontend: the API of exactly one
// frontend, built from the same commit and paired with it by that commit's
// SHA. The frontend calls <prefix>/<sha>/...; Pin answers only its own SHA
// and tells any other frontend to reload. Forward hands the caller's
// credentials to the services the BFF calls, which decide every access, and
// Activity records who still calls a version, so a scheduled job can retire
// it.
package bfftools

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// ReasonVersionRetired is the google.rpc.ErrorInfo reason of a call to a BFF
// version that no longer serves, or never served, the caller's frontend. The
// frontend reloads in full, which loads the current frontend and its BFF.
const ReasonVersionRetired = "BFF_VERSION_RETIRED"

// ErrorDomain is the google.rpc.ErrorInfo domain of ReasonVersionRetired.
const ErrorDomain = "dedo.raveesh.me"

// SHALength is the length of the commit SHA that pairs a frontend with its
// BFF: the first 12 characters of the full SHA, as BuildSHACommand prints
// them. A fixed length keeps one commit one version, however many objects
// the repository grows to.
const SHALength = 12

// BuildSHACommand prints the SHA a frontend and its BFF are built with:
// exactly SHALength characters. Never git rev-parse --short=12, which
// prints more when 12 characters are ambiguous in the repository.
const BuildSHACommand = "git rev-parse HEAD | cut -c1-12"

// CheckSHA reports whether sha is a commit SHA as Pin takes it: SHALength
// lowercase hexadecimal characters.
func CheckSHA(sha string) error {
	if len(sha) != SHALength {
		return fmt.Errorf("bfftools: version %q is not a %d-character commit SHA (%s)", sha, SHALength, BuildSHACommand)
	}
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("bfftools: version %q is not lowercase hexadecimal", sha)
		}
	}
	return nil
}

// checkPrefix reports whether prefix is a path Pin can serve below: empty,
// or starting with a slash and ending without one.
func checkPrefix(prefix string) error {
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/")) {
		return fmt.Errorf("bfftools: prefix %q must start with / and not end with one, such as /shop/bff", prefix)
	}
	return nil
}

// Pin serves next for the frontend built from commit sha. A request for
// <prefix>/<sha>/<rest> reaches next as /<rest>; a request below prefix
// for any other SHA, or none, gets a FAILED_PRECONDITION error whose
// google.rpc.ErrorInfo has the reason ReasonVersionRetired, in the
// protocol the request used, so a Connect client reads it as it reads any
// RPC error. There is no latest alias: a frontend only ever calls its own
// BFF. A request outside prefix gets 404.
//
// Pin panics when sha is not a commit SHA (CheckSHA) or prefix is not a
// path, as a program built without its SHA must not serve; check the SHA
// first to fail with an error instead.
func Pin(sha, prefix string, next http.Handler) http.Handler {
	if err := errors.Join(CheckSHA(sha), checkPrefix(prefix)); err != nil {
		panic(err)
	}
	retired := retiredError()
	errs := connecthttp.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		version, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
		if version != sha || r.URL.Path == prefix {
			// A retired answer must never be cached: the version it
			// names may be deployed again, and the reload must reach it.
			w.Header().Set("Cache-Control", "no-store")
			_ = errs.Write(w, r, retired)
			return
		}
		next.ServeHTTP(w, strip(r, prefix+"/"+sha))
	})
}

// retiredError is the error Pin answers a frontend of another version with.
func retiredError() *connect.Error {
	detail, err := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: ReasonVersionRetired, Domain: ErrorDomain})
	if err != nil {
		panic(err) // an ErrorInfo always marshals
	}
	return connect.NewError(connect.CodeFailedPrecondition,
		"this version of the frontend is retired: reload the page to load the current one").WithDetail(detail)
}

// strip returns a copy of r whose path has lost leading, as
// http.StripPrefix does, leaving r itself unchanged. The rest of the path
// always starts with a slash.
func strip(r *http.Request, leading string) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = new(url.URL)
	*r2.URL = *r.URL
	r2.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, leading), "/")
	if r.URL.RawPath != "" {
		r2.URL.RawPath = "/" + strings.TrimPrefix(strings.TrimPrefix(r.URL.RawPath, leading), "/")
	}
	return r2
}
