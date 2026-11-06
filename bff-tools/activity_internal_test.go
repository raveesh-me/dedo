package bfftools

import (
	"strconv"
	"testing"
	"time"
)

// Past MaxCallers, a new caller still counts as a request, and the report
// says the caller count stopped.
func TestActivityCapsCallers(t *testing.T) {
	a := NewActivity("0123456789ab")
	now := time.Now()
	for i := range MaxCallers {
		a.record(strconv.Itoa(i), now)
	}
	if r := a.Report(); r.Callers != MaxCallers || r.CallersCapped {
		t.Fatalf("at the cap: %+v", r)
	}
	a.record("0", now) // a caller already counted
	if a.Report().CallersCapped {
		t.Fatal("a known caller capped the count")
	}
	a.record("new", now)
	if r := a.Report(); r.Callers != MaxCallers || !r.CallersCapped || r.Requests != MaxCallers+2 {
		t.Errorf("past the cap: %+v", r)
	}
}
