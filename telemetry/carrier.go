package telemetry

import (
	"net"
	"strconv"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/attribute"
)

// headerCarrier lets a propagator read and write a call's headers.
type headerCarrier struct{ h *connect.Header }

func (c headerCarrier) Get(key string) string { return c.h.Get(key) }
func (c headerCarrier) Set(key, value string) { c.h.Set(key, value) }
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, c.h.Len())
	for key := range c.h.All() {
		keys = append(keys, key)
	}
	return keys
}

// addressAttributes splits host:port into an address and a port attribute.
// An address without a port gives the address alone, and none gives none.
func addressAttributes(address string, addressKey, portKey attribute.Key) []attribute.KeyValue {
	if address == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return []attribute.KeyValue{addressKey.String(address)}
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return []attribute.KeyValue{addressKey.String(host)}
	}
	return []attribute.KeyValue{addressKey.String(host), portKey.Int(n)}
}
