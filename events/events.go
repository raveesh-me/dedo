// Package events publishes the events an RPC declares in its contract
// (dedo.v1.method's emits) to an event bus.
//
// A service only waits to be called; it never watches or subscribes.
// Events move outward only, and say nothing but that something happened
// to a resource: a type, the resource's name, an ID and a time. Consumers
// fetch the resource through the API, with their own token and scopes.
//
// The bus is a driver (package driver) that the service injects, as
// database/sql takes a database driver; dedo has no opinion on which. Once
// the driver acknowledges an event, it delivers it at least once. Until
// then, Publish retries with exponential backoff, within the request's
// deadline. A handler that returns Publish's error asks the caller to
// retry the command, which closes the gap between the commit and the
// acknowledgement without the service holding any state.
//
// An event's ID stays the same across the retries of one Publish, so a
// consumer can recognise the bus redelivering it. A retried command runs
// its handler again and publishes a new event with a new ID; give it the
// IdempotencyKey option, such as the request's request_id, and the
// events of every retry carry the same key.
package events

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"regexp"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/raveesh-me/dedo/events/driver"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/policy"
)

// ReasonNotPublished is the ErrorInfo reason of the error Publish returns
// when the bus did not acknowledge an event in time, with code
// UNAVAILABLE. An RPC whose handler returns that error declares it in its
// errors.
const ReasonNotPublished = "EVENT_NOT_PUBLISHED"

// The causes of the INTERNAL error Publish returns when it refuses an
// event, for errors.Is.
var (
	// ErrOutsideRPC: Publish was called outside an RPC served behind the
	// policy interceptor, so no contract says which events it may publish.
	ErrOutsideRPC = errors.New("events: published outside an RPC")
	// ErrUndeclared: the RPC does not declare the event type in emits.
	ErrUndeclared = errors.New("events: event type not declared")
	// ErrNoSubject: the event names no resource.
	ErrNoSubject = errors.New("events: no subject")
	// ErrBadIdempotencyKey: the IdempotencyKey option is empty, longer
	// than 128 characters, or holds a space or a character that is not
	// printable ASCII.
	ErrBadIdempotencyKey = errors.New("events: bad idempotency key")
)

// validKey is a key a consumer can store and compare as it is: printable
// ASCII without spaces, such as a UUID.
var validKey = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)

// Option changes one event Publish sends.
type Option func(*options)

// options are what Options set on one Publish.
type options struct {
	key    string
	keySet bool
}

// IdempotencyKey gives the event a key that stays the same when the
// caller retries the command, such as the request's request_id (AIP-155).
// The event's ID is new on every Publish; the key is what lets a consumer
// recognise the event of a retried command as one it has handled.
func IdempotencyKey(key string) Option {
	return func(o *options) { o.key, o.keySet = key, true }
}

// flowIDExtension is the CloudEvents extension that carries the flow ID
// of the request that caused the event.
const flowIDExtension = "flowid"

// The default retry policy: about a dozen attempts in five seconds.
const (
	defaultInitial    = 50 * time.Millisecond
	defaultMax        = time.Second
	defaultMultiplier = 2
	defaultTimeout    = 5 * time.Second
)

// Retry is how Publish retries an event the driver did not acknowledge.
// Zero fields take the defaults.
type Retry struct {
	// Initial is the wait before the first retry. Default 50ms.
	Initial time.Duration
	// Max caps the wait between two attempts. Default 1s.
	Max time.Duration
	// Multiplier grows the wait after each attempt. At least 1; default 2.
	Multiplier float64
	// Timeout bounds Publish when its context has no earlier deadline,
	// as a request without a timeout has. Default 5s.
	Timeout time.Duration
}

// Config wires a publisher to a bus.
type Config struct {
	// Driver hands events to the bus. Required.
	Driver driver.Driver
	// Source identifies the service in every event, such as
	// "//orders.acme.example.com". Required.
	Source string
	// Retry is the retry policy.
	Retry Retry
	// Log records every event that is not published. Default
	// slog.Default().
	Log *slog.Logger
}

// Publisher publishes declared events through a driver. It is safe for
// concurrent use.
type Publisher struct {
	driver driver.Driver
	source string
	retry  Retry
	log    *slog.Logger
	now    func() time.Time
	wait   func(context.Context, time.Duration) error
	jitter func(time.Duration) time.Duration
}

// New returns a publisher, failing on a missing driver or source or an
// invalid retry policy.
func New(cfg Config) (*Publisher, error) {
	if cfg.Driver == nil {
		return nil, errors.New("events: no driver")
	}
	if cfg.Source == "" {
		return nil, errors.New("events: no source")
	}
	retry, err := withDefaults(cfg.Retry)
	if err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Publisher{
		driver: cfg.Driver,
		source: cfg.Source,
		retry:  retry,
		log:    log,
		now:    time.Now,
		wait:   sleep,
		jitter: fullJitter,
	}, nil
}

func withDefaults(r Retry) (Retry, error) {
	if r.Initial < 0 || r.Max < 0 || r.Timeout < 0 {
		return r, errors.New("events: negative retry duration")
	}
	if r.Multiplier != 0 && r.Multiplier < 1 {
		return r, fmt.Errorf("events: retry multiplier %v is below 1", r.Multiplier)
	}
	if r.Initial == 0 {
		r.Initial = defaultInitial
	}
	if r.Max == 0 {
		r.Max = max(defaultMax, r.Initial)
	}
	if r.Multiplier == 0 {
		r.Multiplier = defaultMultiplier
	}
	if r.Timeout == 0 {
		r.Timeout = defaultTimeout
	}
	if r.Max < r.Initial {
		return r, fmt.Errorf("events: retry max %v is below initial %v", r.Max, r.Initial)
	}
	return r, nil
}

// Publish hands one event to the bus and returns it once the driver
// acknowledges it. eventType must be one the RPC being served declares in
// emits; subject is the resource name of the resource it is about, such
// as "orders/123". Call it from a handler, after the change it announces
// has committed.
//
// Publish retries until the driver acknowledges the event, the driver
// reports a permanent error, or ctx is done (bounded by Retry.Timeout).
// When the bus does not acknowledge in time, it returns an UNAVAILABLE
// error with reason ReasonNotPublished, which the handler can return so
// the caller retries the command. A refused event (outside an RPC, an
// undeclared type, no subject) or a permanent driver error is INTERNAL.
func (p *Publisher) Publish(ctx context.Context, eventType, subject string, opts ...Option) (driver.Event, error) {
	e := driver.Event{
		ID:      newID(),
		Type:    eventType,
		Source:  p.source,
		Subject: subject,
		Time:    p.now().UTC().Round(0),
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	e.IdempotencyKey = o.key
	if err := refused(ctx, e, o.keySet); err != nil {
		return driver.Event{}, connect.NewError(connect.CodeInternal, "event refused").WithCause(err)
	}
	if id := flowid.From(ctx); id != "" {
		e.Extensions = map[string]string{flowIDExtension: id}
	}
	ctx, cancel := context.WithTimeout(ctx, p.retry.Timeout)
	defer cancel()
	attempts, err := p.deliver(ctx, e)
	if err == nil {
		return e, nil
	}
	p.log.ErrorContext(ctx, "event not published",
		"type", e.Type, "subject", e.Subject, "id", e.ID, "attempts", attempts,
		"flow_id", flowid.From(ctx), "error", err)
	if driver.IsPermanent(err) {
		return driver.Event{}, connect.NewError(connect.CodeInternal, "event not published").WithCause(err)
	}
	return driver.Event{}, notPublished(err)
}

// refused says why Publish refuses e, or nil. keySet is set when the
// IdempotencyKey option ran, so an empty key is a mistake, not an absence.
func refused(ctx context.Context, e driver.Event, keySet bool) error {
	if err := declared(ctx, e.Type, e.Subject); err != nil {
		return err
	}
	if keySet && !validKey.MatchString(e.IdempotencyKey) {
		return fmt.Errorf("%w: %q; use printable ASCII without spaces, at most 128 characters, such as the request's request_id", ErrBadIdempotencyKey, e.IdempotencyKey)
	}
	return nil
}

// declared checks that the RPC being served declares eventType.
func declared(ctx context.Context, eventType, subject string) error {
	m, ok := policy.MethodFrom(ctx)
	if !ok {
		return fmt.Errorf("%w: %s; a service publishes only from a handler, behind the policy interceptor", ErrOutsideRPC, eventType)
	}
	if !slices.Contains(m.Rules.GetEmits(), eventType) {
		return fmt.Errorf("%w: %s does not declare %q in emits", ErrUndeclared, m.Procedure, eventType)
	}
	if subject == "" {
		return fmt.Errorf("%w: %s; name the resource it is about", ErrNoSubject, eventType)
	}
	return nil
}

// deliver tries until the driver acknowledges e, reports a permanent
// error, or ctx is done. It returns the number of attempts and the last
// error, joined with the context's when that ended the retries.
func (p *Publisher) deliver(ctx context.Context, e driver.Event) (int, error) {
	backoff := p.retry.Initial
	for attempt := 1; ; attempt++ {
		err := p.driver.Publish(ctx, attemptOf(e))
		if err == nil || driver.IsPermanent(err) {
			return attempt, err
		}
		if werr := p.wait(ctx, p.jitter(backoff)); werr != nil {
			return attempt, errors.Join(err, werr)
		}
		backoff = min(time.Duration(float64(backoff)*p.retry.Multiplier), p.retry.Max)
	}
}

// attemptOf returns a copy of e for one attempt, so a driver that changes
// its extensions changes neither the next attempt nor the event Publish
// returns.
func attemptOf(e driver.Event) driver.Event {
	e.Extensions = maps.Clone(e.Extensions)
	return e
}

// notPublished is the error a handler returns so the caller retries.
func notPublished(cause error) error {
	err := connect.NewError(connect.CodeUnavailable, "event not published; retry the request").WithCause(cause)
	detail, derr := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: ReasonNotPublished})
	if derr != nil {
		return err
	}
	return err.WithDetail(detail)
}

// sleep waits for d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fullJitter spreads retries from many replicas: a random wait in [0, d].
func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d) + 1))
}

// newID returns a random event ID, unique enough that consumers can use
// it to recognise a duplicate.
func newID() string {
	b := make([]byte, 16)
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = cryptorand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
