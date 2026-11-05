package events

import (
	"context"
	"time"
)

// WithClock returns a copy of p that reads the time from now and waits
// with wait, so tests run without sleeping.
func WithClock(p *Publisher, now func() time.Time, wait func(context.Context, time.Duration) error) *Publisher {
	c := *p
	c.now, c.wait = now, wait
	return &c
}

// WithoutJitter returns a copy of p that waits the full backoff, so tests
// can check it.
func WithoutJitter(p *Publisher) *Publisher {
	c := *p
	c.jitter = func(d time.Duration) time.Duration { return d }
	return &c
}
