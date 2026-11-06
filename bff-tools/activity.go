package bfftools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// MaxCallers is how many distinct callers Activity tells apart. Past it, a
// version is plainly in use, and Report says the count stopped.
const MaxCallers = 1 << 20

// Activity records the requests one BFF version serves, so a scheduled job
// can decide when to retire the version and write what it last saw. Wrap
// only the version's own routes with Middleware, inside Pin, so health
// probes and retired callers are not counted.
type Activity struct {
	sha string

	mu       sync.Mutex
	requests uint64
	callers  map[uint64]struct{}
	capped   bool
	first    time.Time
	last     time.Time
}

// Report is what Activity has recorded.
type Report struct {
	// SHA is the version, the commit the frontend and BFF were built from.
	SHA string `json:"sha"`
	// Requests counts every request served.
	Requests uint64 `json:"requests"`
	// Callers counts distinct Authorization values. A refreshed token
	// counts again; a request without one counts in Requests only.
	Callers int `json:"callers"`
	// CallersCapped is set once Callers reached MaxCallers and stopped.
	CallersCapped bool `json:"callersCapped,omitempty"`
	// FirstRequest and LastRequest are the times of the first and the
	// latest request; both are absent before any.
	FirstRequest time.Time `json:"firstRequest,omitzero"`
	LastRequest  time.Time `json:"lastRequest,omitzero"`
}

// NewActivity returns an empty record of the version sha.
func NewActivity(sha string) *Activity {
	return &Activity{sha: sha, callers: map[uint64]struct{}{}}
}

// Middleware counts each request, then serves next. A caller is known only
// by a SHA-256 of its Authorization header: the token itself is never kept.
func (a *Activity) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.record(r.Header.Get(Authorization), time.Now().UTC())
		next.ServeHTTP(w, r)
	})
}

func (a *Activity) record(authorization string, now time.Time) {
	var caller uint64
	if authorization != "" {
		sum := sha256.Sum256([]byte(authorization))
		caller = binary.BigEndian.Uint64(sum[:8])
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests++
	if a.first.IsZero() {
		a.first = now
	}
	a.last = now
	if authorization == "" {
		return
	}
	if _, seen := a.callers[caller]; !seen {
		if len(a.callers) >= MaxCallers {
			a.capped = true
			return
		}
		a.callers[caller] = struct{}{}
	}
}

// Report returns what has been recorded so far.
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

// Handler serves Report as JSON to GET, for the job that retires the
// version. Serve it outside Pin's prefix, where the frontend's route does
// not reach.
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
