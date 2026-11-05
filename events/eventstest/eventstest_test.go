package eventstest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/raveesh-me/dedo/events/driver"
	"github.com/raveesh-me/dedo/events/eventstest"
)

func TestDriver(t *testing.T) {
	d := eventstest.New()
	busy := errors.New("busy")
	d.FailNext(busy)
	e := driver.Event{ID: "1", Type: "acme.orders.order.cancelled", Subject: "orders/1"}
	if err := d.Publish(t.Context(), e); !errors.Is(err, busy) {
		t.Fatalf("first attempt: %v, want %v", err, busy)
	}
	if err := d.Publish(t.Context(), e); err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if got := len(d.Attempts()); got != 2 {
		t.Fatalf("%d attempts, want 2", got)
	}
	if got := d.Events(); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("acknowledged %v, want the one event", got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.Publish(ctx, e); !errors.Is(err, context.Canceled) {
		t.Fatalf("done context: %v, want %v", err, context.Canceled)
	}
}
