package bfftools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/maphash"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MaxCallers is how many distinct callers Activity tells apart, holding one
// 8-byte hash each, so a version's count never takes more than a few
// hundred kilobytes. Past it, a version is plainly in use, and Report says
// the count stopped.
const MaxCallers = 10_000

// MaxProcedureLength is the longest procedure an activity log line holds;
// a longer request path is cut, so a caller cannot grow the log.
const MaxProcedureLength = 256

// ActivityMessage is the message of the log line Activity writes for each
// request.
const ActivityMessage = "bff request"

// Activity records the requests one BFF version serves, so a scheduled job
// can decide when to retire the version and write what it last saw.
//
// The durable record is the log: Middleware writes one structured line per
// request, ActivityMessage at level INFO with the attributes sha, procedure
// (the path below the version, such as /shop.web.v1.WebService/GetPage)
// and, for a request with credentials, caller. Aggregate those lines by sha
// in the log platform for the requests, distinct callers and last request
// of a version over any window. Report and Handler count the same in
// memory, but only for this process since it started: a restart, or a
// second replica, starts or keeps its own counts.
//
// Wrap only the version's own routes with Middleware, inside Pin, so health
// probes and retired callers are not counted.
type Activity struct {
	sha       string
	log       *slog.Logger
	callerKey func(*http.Request) string
	seed      maphash.Seed

	mu       sync.Mutex
	requests uint64
	callers  map[uint64]struct{}
	capped   bool
	first    time.Time
	last     time.Time
}

// ActivityOptions configure an Activity. The zero value logs to
// slog.Default() and tells callers apart by AuthorizationHash.
type ActivityOptions struct {
	// Log receives one line per request. Nil means slog.Default().
	Log *slog.Logger
	// CallerKey derives who a request comes from, such as the user and
	// location in its verified token's claims or its tenancy headers, so
	// a refreshed token is the same caller. It is called only for a
	// request with an Authorization header; a request without one, or
	// for which it returns "", is counted and logged with no caller. Nil
	// means AuthorizationHash, under which a refreshed token is a new
	// caller. The key is written to the log as it is: never return the
	// token or another secret.
	CallerKey func(*http.Request) string
}

// Report is what Activity has recorded in this process since it started.
// It resets when the process restarts; the log lines are the durable
// record.
type Report struct {
	// SHA is the version, the commit the frontend and BFF were built from.
	SHA string `json:"sha"`
	// Requests counts every request served.
	Requests uint64 `json:"requests"`
	// Callers counts distinct caller keys (ActivityOptions.CallerKey). A
	// request without Authorization counts in Requests only.
	Callers int `json:"callers"`
	// CallersCapped is set once Callers reached MaxCallers and stopped.
	CallersCapped bool `json:"callersCapped,omitempty"`
	// FirstRequest and LastRequest are the times of the first and the
	// latest request; both are absent before any.
	FirstRequest time.Time `json:"firstRequest,omitzero"`
	LastRequest  time.Time `json:"lastRequest,omitzero"`
}

// NewActivity returns an empty record of the version sha.
func NewActivity(sha string, opts ActivityOptions) *Activity {
	a := &Activity{
		sha:       sha,
		log:       opts.Log,
		callerKey: opts.CallerKey,
		seed:      maphash.MakeSeed(),
		callers:   map[uint64]struct{}{},
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	if a.callerKey == nil {
		a.callerKey = AuthorizationHash
	}
	return a
}

// AuthorizationHash is the default caller key: the first 8 bytes of a
// SHA-256 of the request's Authorization header, in hexadecimal, or "" when
// it has none. The token itself is never kept or logged.
func AuthorizationHash(r *http.Request) string {
	authorization := r.Header.Get(Authorization)
	if authorization == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(authorization))
	return hex.EncodeToString(sum[:8])
}

// Middleware counts and logs each request, then serves next.
func (a *Activity) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := ""
		if r.Header.Get(Authorization) != "" {
			caller = a.callerKey(r)
		}
		a.record(caller, time.Now().UTC())
		procedure := r.URL.Path
		if len(procedure) > MaxProcedureLength {
			procedure = procedure[:MaxProcedureLength]
		}
		attrs := []slog.Attr{slog.String("sha", a.sha), slog.String("procedure", procedure)}
		if caller != "" {
			attrs = append(attrs, slog.String("caller", caller))
		}
		a.log.LogAttrs(r.Context(), slog.LevelInfo, ActivityMessage, attrs...)
		next.ServeHTTP(w, r)
	})
}

// record counts a request at now from caller, "" for none. Callers are kept
// as 8-byte hashes, so memory stays bounded whatever the keys are.
func (a *Activity) record(caller string, now time.Time) {
	var key uint64
	if caller != "" {
		key = maphash.String(a.seed, caller)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests++
	if a.first.IsZero() {
		a.first = now
	}
	a.last = now
	if caller == "" {
		return
	}
	if _, seen := a.callers[key]; !seen {
		if len(a.callers) >= MaxCallers {
			a.capped = true
			return
		}
		a.callers[key] = struct{}{}
	}
}

// Report returns what has been recorded in this process so far.
func (a *Activity) Report() Report {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Report{
		SHA:           a.sha,
		Requests:      a.requests,
		Callers:       len(a.callers),
		CallersCapped: a.capped,
		FirstRequest:  a.first,
		LastRequest:   a.last,
	}
}

// Handler serves Report as JSON to GET. Its counts are this process's since
// it started, so a job that retires the version reads the log for the
// whole window, and this for a quick look. Serve it outside Pin's prefix,
// where the frontend's route does not reach.
func (a *Activity) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := json.Marshal(a.Report())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(append(body, '\n'))
	})
}
