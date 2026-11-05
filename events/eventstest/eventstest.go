// Package eventstest provides an in-memory event driver for tests: it
// records what a service publishes and can fail on demand.
package eventstest

import (
	"context"
	"slices"
	"sync"

	"github.com/raveesh-me/dedo/events/driver"
)

// Driver records every attempt to publish and acknowledges the ones it
// does not fail. It is safe for concurrent use.
type Driver struct {
	mu       sync.Mutex
	attempts []driver.Event
	acked    []driver.Event
	failures []error
}

var _ driver.Driver = (*Driver)(nil)

// New returns a driver that acknowledges every event.
func New() *Driver { return &Driver{} }

// FailNext makes the next len(errs) attempts return errs, in order.
func (d *Driver) FailNext(errs ...error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failures = append(d.failures, errs...)
}

// Publish records the attempt, then fails it if FailNext asked to, or
// acknowledges it.
func (d *Driver) Publish(ctx context.Context, e driver.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attempts = append(d.attempts, e)
	if len(d.failures) > 0 {
		err := d.failures[0]
		d.failures = d.failures[1:]
		return err
	}
	d.acked = append(d.acked, e)
	return nil
}

// Events returns the acknowledged events, in the order they arrived.
func (d *Driver) Events() []driver.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.acked)
}

// Attempts returns every event handed to Publish, failed ones included.
func (d *Driver) Attempts() []driver.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.attempts)
}
