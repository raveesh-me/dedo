package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/raveesh-me/dedo/telemetry/objectives"
)

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
		metric.WithExplicitBucketBoundaries(objectives.DurationBuckets...))
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", name, err)
	}
	return h, nil
}
