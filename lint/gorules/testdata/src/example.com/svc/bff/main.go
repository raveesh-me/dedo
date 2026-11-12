// Package bff is a backend for frontend's server: it needs only the flow ID
// interceptor.
package bff

import (
	"connectrpc.com/connect/v2"

	"github.com/raveesh-me/dedo/flowid"
)

func serve() {
	connect.NewServer(flowid.Interceptor())
	connect.NewServer() // want `has no flowid.Interceptor\(\)`
}
