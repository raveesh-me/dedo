// Package driver defines what an event bus implements to carry dedo's
// events, as database/sql/driver does for databases. Services do not use
// it directly: they publish through package events, which adds what every
// driver would otherwise repeat, such as retries.
//
// dedo has no opinion on the bus. A driver for Pub/Sub, Kafka, NATS or an
// HTTP sink maps an Event onto the bus's own message attributes or
// headers. An event has no body.
package driver

import (
	"context"
	"errors"
	"time"
)

// Event is a notification that something happened to a resource. It
// carries no data: consumers fetch the resource through the API, with
// their own token and scopes. The fields are CloudEvents' required and
// optional context attributes.
type Event struct {
	// ID identifies the event. It stays the same across the publisher's
	// retries, so a consumer can recognise the bus redelivering it. A
	// command the caller retries publishes a new event, with a new ID;
	// IdempotencyKey is what stays the same then.
	ID string
	// Type is the event type the RPC declares in emits, such as
	// "acme.orders.order.cancelled".
	Type string
	// Source identifies the service that published the event, such as
	// "//orders.acme.example.com".
	Source string
	// Subject is the resource name of the resource the event is about,
	// such as "orders/123".
	Subject string
	// Time is when the event was published, in UTC.
	Time time.Time
	// IdempotencyKey, when set, is the same for every event a retried
	// command publishes, such as the request's request_id, so a consumer
	// can drop the events of a retry. A driver carries it as the
	// CloudEvents extension idempotencykey, or the bus's own field for
	// one, such as a Kafka record key.
	IdempotencyKey string
	// Extensions are further attributes, such as the flow ID of the
	// request that caused the event (flowid) and its trace context
	// (traceparent and tracestate, the CloudEvents Distributed Tracing
	// extension). Names are CloudEvents extension names: lowercase letters
	// and digits. A driver carries every one, so a consumer can continue
	// the trace.
	Extensions map[string]string
}

// Driver hands events to a bus.
type Driver interface {
	// Publish hands e to the bus. A nil error is an acknowledgement: the
	// bus holds the event durably and will deliver it at least once. An
	// error means the bus may not have it; wrap the error with Permanent
	// when trying again cannot help, such as for a topic that does not
	// exist. Publish must return when ctx is done, and must not change e.
	Publish(ctx context.Context, e Event) error
}

// permanentError marks an error that a retry cannot fix.
type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

// Permanent marks err as one that trying again cannot fix, so the
// publisher stops retrying. It returns nil for a nil err.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err, or an error it wraps, was marked with
// Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}
