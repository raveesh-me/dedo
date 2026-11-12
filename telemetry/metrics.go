package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// durationBuckets are the bucket boundaries, in seconds, the semantic
// conventions advise for RPC durations.
var durationBuckets = []float64{
	0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
}

// The duration metrics of the semantic conventions for RPC.
const (
	serverDuration = "rpc.server.call.duration"
	clientDuration = "rpc.client.call.duration"
)

// newDuration returns the named RPC duration histogram.
func newDuration(meter metric.Meter, name, description string) (metric.Float64Histogram, error) {
	h, err := meter.Float64Histogram(name,
		metric.WithUnit("s"),
		metric.WithDescription(description),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", name, err)
	}
	return h, nil
}
