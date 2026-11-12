package app

import (
	"testing"

	"connectrpc.com/connect/v2"
)

// Tests build bare servers: no finding.
func TestServer(t *testing.T) {
	connect.NewServer()
}
