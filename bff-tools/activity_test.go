package bfftools_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	bfftools "github.com/raveesh-me/dedo/bff-tools"
)

func TestActivity(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string // one request each; "" sends no Authorization
		requests uint64
		callers  int
	}{
		{"none", nil, 0, 0},
		{"one caller", []string{"Bearer a", "Bearer a"}, 2, 1},
		{"distinct callers", []string{"Bearer a", "Bearer b", "Bearer a", "Bearer c"}, 4, 3},
		{"anonymous requests count only as requests", []string{"", "", "Bearer a"}, 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := bfftools.NewActivity(sha)
			h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			before := time.Now().UTC()
			for _, token := range tt.tokens {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				if token != "" {
					req.Header.Set("Authorization", token)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusNoContent {
					t.Fatalf("next was not served: %d", rec.Code)
				}
			}
			after := time.Now().UTC()
			r := a.Report()
			if r.SHA != sha || r.Requests != tt.requests || r.Callers != tt.callers || r.CallersCapped {
				t.Errorf("report = %+v, want %d requests from %d callers", r, tt.requests, tt.callers)
			}
			if tt.requests == 0 {
				if !r.FirstRequest.IsZero() || !r.LastRequest.IsZero() {
					t.Errorf("times = %v, %v, want none", r.FirstRequest, r.LastRequest)
				}
				return
			}
			if r.FirstRequest.Before(before) || r.LastRequest.After(after) || r.LastRequest.Before(r.FirstRequest) {
				t.Errorf("times = %v, %v, want within %v..%v in order", r.FirstRequest, r.LastRequest, before, after)
			}
		})
	}
}

func TestActivityConcurrent(t *testing.T) {
	a := bfftools.NewActivity(sha)
	h := a.Middleware(http.NotFoundHandler())
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+string(rune('a'+i%5)))
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
	wg.Wait()
	if r := a.Report(); r.Requests != 50 || r.Callers != 5 {
		t.Errorf("report = %+v, want 50 requests from 5 callers", r)
	}
}

func TestActivityHandler(t *testing.T) {
	a := bfftools.NewActivity(sha)
	token := "Bearer secret-token"
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", token)
	a.Middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activity", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Errorf("the report shows the token: %s", rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sha", "requests", "callers", "firstRequest", "lastRequest"} {
		if _, ok := got[key]; !ok {
			t.Errorf("report lacks %s: %s", key, rec.Body)
		}
	}
	if got["sha"] != sha || got["requests"] != 1.0 || got["callers"] != 1.0 {
		t.Errorf("report = %s", rec.Body)
	}

	empty := httptest.NewRecorder()
	bfftools.NewActivity(sha).Handler().ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/activity", nil))
	if strings.Contains(empty.Body.String(), "Request") {
		t.Errorf("an empty report has request times: %s", empty.Body)
	}

	post := httptest.NewRecorder()
	a.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/activity", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", post.Code)
	}
}
