package events_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/raveesh-me/dedo/events"
	"github.com/raveesh-me/dedo/events/driver"
	"github.com/raveesh-me/dedo/events/eventstest"
	"github.com/raveesh-me/dedo/flowid"
	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
	"github.com/raveesh-me/dedo/policy"
)

const (
	cancelled = "acme.orders.order.cancelled"
	source    = "//orders.acme.example.com"
	subject   = "orders/123"
)

var epoch = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// fixture is a publisher over a fake driver, with a fake clock that
// records every wait instead of sleeping.
type fixture struct {
	pub    *events.Publisher
	driver *eventstest.Driver
	waits  *[]time.Duration
	log    *bytes.Buffer
}

func newFixture(t *testing.T, retry events.Retry) fixture {
	t.Helper()
	d := eventstest.New()
	var log bytes.Buffer
	pub, err := events.New(events.Config{
		Driver: d,
		Source: source,
		Retry:  retry,
		Log:    slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	waits := &[]time.Duration{}
	pub = events.WithClock(pub, func() time.Time { return epoch }, func(ctx context.Context, d time.Duration) error {
		*waits = append(*waits, d)
		return ctx.Err()
	})
	return fixture{pub: pub, driver: d, waits: waits, log: &log}
}

// inMethod returns a context inside an RPC that declares emits.
func inMethod(ctx context.Context, emits ...string) context.Context {
	return policy.WithMethod(ctx, policy.Method{
		Procedure: "/acme.orders.v1.OrderService/CancelOrder",
		Rules:     &dedov1.MethodRules{Emits: emits},
	})
}

func TestPublish(t *testing.T) {
	f := newFixture(t, events.Retry{})
	ctx := flowid.WithID(inMethod(t.Context(), cancelled), "flow-1")
	got, err := f.pub.Publish(ctx, cancelled, subject)
	if err != nil {
		t.Fatal(err)
	}
	acked := f.driver.Events()
	if len(acked) != 1 {
		t.Fatalf("driver acknowledged %d events, want 1", len(acked))
	}
	e := acked[0]
	if e.ID == "" || e.ID != got.ID {
		t.Errorf("ID = %q, returned %q", e.ID, got.ID)
	}
	if e.Type != cancelled || e.Source != source || e.Subject != subject || !e.Time.Equal(epoch) {
		t.Errorf("event = %+v", e)
	}
	if e.Extensions["flowid"] != "flow-1" {
		t.Errorf("flowid extension = %q", e.Extensions["flowid"])
	}
}

func TestPublishRefuses(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		typ     string
		subject string
		want    error
	}{
		{"outside an RPC", context.Background(), cancelled, subject, events.ErrOutsideRPC},
		{"undeclared type", inMethod(context.Background(), "acme.orders.order.placed"), cancelled, subject, events.ErrUndeclared},
		{"no subject", inMethod(context.Background(), cancelled), cancelled, "", events.ErrNoSubject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, events.Retry{})
			_, err := f.pub.Publish(tt.ctx, tt.typ, tt.subject)
			if connect.CodeOf(err) != connect.CodeInternal || !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want internal error caused by %v", err, tt.want)
			}
			if n := len(f.driver.Attempts()); n != 0 {
				t.Fatalf("driver saw %d attempts, want none", n)
			}
		})
	}
}

func TestPublishRetries(t *testing.T) {
	f := newFixture(t, events.Retry{Initial: 10 * time.Millisecond, Max: 30 * time.Millisecond, Multiplier: 2})
	f.pub = events.WithoutJitter(f.pub)
	busy := errors.New("broker busy")
	f.driver.FailNext(busy, busy, busy, busy)
	if _, err := f.pub.Publish(inMethod(t.Context(), cancelled), cancelled, subject); err != nil {
		t.Fatal(err)
	}
	attempts := f.driver.Attempts()
	if len(attempts) != 5 {
		t.Fatalf("%d attempts, want 5", len(attempts))
	}
	for _, a := range attempts[1:] {
		if a.ID != attempts[0].ID {
			t.Fatalf("retry changed the ID from %q to %q", attempts[0].ID, a.ID)
		}
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 30 * time.Millisecond}
	if !equalDurations(*f.waits, want) {
		t.Fatalf("waits = %v, want %v", *f.waits, want)
	}
}

func TestPublishJitter(t *testing.T) {
	f := newFixture(t, events.Retry{Initial: 10 * time.Millisecond, Max: 10 * time.Millisecond, Multiplier: 2})
	f.driver.FailNext(errors.New("busy"), errors.New("busy"), errors.New("busy"))
	if _, err := f.pub.Publish(inMethod(t.Context(), cancelled), cancelled, subject); err != nil {
		t.Fatal(err)
	}
	for _, w := range *f.waits {
		if w < 0 || w > 10*time.Millisecond {
			t.Fatalf("wait %v outside [0, 10ms]", w)
		}
	}
}

func TestPublishPermanent(t *testing.T) {
	f := newFixture(t, events.Retry{})
	missing := errors.New("no such topic")
	f.driver.FailNext(driver.Permanent(missing))
	_, err := f.pub.Publish(inMethod(t.Context(), cancelled), cancelled, subject)
	if connect.CodeOf(err) != connect.CodeInternal || !errors.Is(err, missing) {
		t.Fatalf("err = %v, want internal error caused by %v", err, missing)
	}
	if n := len(f.driver.Attempts()); n != 1 {
		t.Fatalf("%d attempts, want 1", n)
	}
	if !strings.Contains(f.log.String(), "event not published") {
		t.Fatalf("log lacks the dropped event: %s", f.log)
	}
}

func TestPublishGivesUpAtDeadline(t *testing.T) {
	f := newFixture(t, events.Retry{})
	busy := errors.New("broker busy")
	f.driver.FailNext(busy)
	ctx, cancel := context.WithCancel(inMethod(t.Context(), cancelled))
	f.pub = events.WithClock(f.pub, func() time.Time { return epoch }, func(context.Context, time.Duration) error {
		cancel() // the request's deadline passes while waiting
		return context.DeadlineExceeded
	})
	_, err := f.pub.Publish(ctx, cancelled, subject)
	if connect.CodeOf(err) != connect.CodeUnavailable || !errors.Is(err, busy) {
		t.Fatalf("err = %v, want unavailable caused by %v", err, busy)
	}
	if reason := errorReason(err); reason != events.ReasonNotPublished {
		t.Fatalf("reason = %q, want %q", reason, events.ReasonNotPublished)
	}
	for _, want := range []string{"event not published", cancelled, subject} {
		if !strings.Contains(f.log.String(), want) {
			t.Fatalf("log lacks %q: %s", want, f.log)
		}
	}
}

func TestPublishBoundsAnOpenContext(t *testing.T) {
	f := newFixture(t, events.Retry{Timeout: time.Millisecond})
	var deadline time.Time
	f.pub = events.WithClock(f.pub, func() time.Time { return epoch }, func(ctx context.Context, _ time.Duration) error {
		deadline, _ = ctx.Deadline()
		<-ctx.Done()
		return ctx.Err()
	})
	f.driver.FailNext(errors.New("busy"))
	if _, err := f.pub.Publish(inMethod(context.Background(), cancelled), cancelled, subject); err == nil {
		t.Fatal("published past the timeout")
	}
	if deadline.IsZero() {
		t.Fatal("a context without a deadline was not bounded by Retry.Timeout")
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		cfg  events.Config
		want string
	}{
		{"no driver", events.Config{Source: source}, "no driver"},
		{"no source", events.Config{Driver: eventstest.New()}, "no source"},
		{"bad retry", events.Config{Driver: eventstest.New(), Source: source, Retry: events.Retry{Multiplier: 0.5}}, "multiplier"},
		{"max below initial", events.Config{Driver: eventstest.New(), Source: source, Retry: events.Retry{Initial: time.Second, Max: time.Millisecond}}, "below initial"},
		{"negative duration", events.Config{Driver: eventstest.New(), Source: source, Retry: events.Retry{Timeout: -time.Second}}, "negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := events.New(tt.cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func errorReason(err error) string {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return ""
	}
	for _, d := range cerr.Details() {
		if v, err := connectproto.UnmarshalErrorDetail(d); err == nil {
			if info, ok := v.(*errdetails.ErrorInfo); ok {
				return info.GetReason()
			}
		}
	}
	return ""
}

// mutatingDriver changes every event it is handed, as a careless driver
// might, and fails the first attempt.
type mutatingDriver struct{ attempts []driver.Event }

func (m *mutatingDriver) Publish(_ context.Context, e driver.Event) error {
	m.attempts = append(m.attempts, e)
	e.Extensions["added"] = "by the driver"
	if len(m.attempts) == 1 {
		return errors.New("busy")
	}
	return nil
}

func TestPublishIsolatesAttempts(t *testing.T) {
	d := &mutatingDriver{}
	pub, err := events.New(events.Config{Driver: d, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	local := time.FixedZone("UTC+5:30", 5*3600+1800)
	pub = events.WithClock(pub, func() time.Time { return epoch.In(local) }, func(context.Context, time.Duration) error { return nil })
	ctx := flowid.WithID(inMethod(t.Context(), cancelled), "flow-1")
	got, err := pub.Publish(ctx, cancelled, subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Extensions["added"]; ok {
		t.Fatal("the driver changed the event Publish returned")
	}
	if len(d.attempts) != 2 {
		t.Fatalf("%d attempts, want 2", len(d.attempts))
	}
	if got.Time.Location() != time.UTC || !got.Time.Equal(epoch) {
		t.Fatalf("time = %v, want %v in UTC", got.Time, epoch)
	}
}
