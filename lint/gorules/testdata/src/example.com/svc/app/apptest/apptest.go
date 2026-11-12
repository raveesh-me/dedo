// Package apptest holds test helpers, which build bare servers: no finding.
package apptest

import (
	"testing"

	"connectrpc.com/connect/v2"
)

func Server(t testing.TB) *connect.Server { return connect.NewServer() }
