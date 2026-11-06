package bfftools_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
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
			a := bfftools.NewActivity(sha, quiet())
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
	a := bfftools.NewActivity(sha, quiet())
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
	a := bfftools.NewActivity(sha, quiet())
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
	bfftools.NewActivity(sha, quiet()).Handler().ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/activity", nil))
	if strings.Contains(empty.Body.String(), "Request") {
		t.Errorf("an empty report has request times: %s", empty.Body)
	}

	post := httptest.NewRecorder()
	a.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/activity", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", post.Code)
	}
}

// quiet is ActivityOptions whose log goes nowhere.
func quiet() bfftools.ActivityOptions {
	return bfftools.ActivityOptions{Log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
}

// logLines decodes the JSON log lines in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		out = append(out, line)
	}
	return out
}

// Each request writes one structured log line, the durable record of a
// version's activity: its SHA, the procedure and, when it carried
// credentials, a caller key that is never the token.
func TestActivityLogsEachRequest(t *testing.T) {
	var buf bytes.Buffer
	a := bfftools.NewActivity(sha, bfftools.ActivityOptions{Log: slog.New(slog.NewJSONHandler(&buf, nil))})
	h := a.Middleware(http.NotFoundHandler())
	for _, token := range []string{"Bearer secret-a", "", "Bearer secret-a", "Bearer secret-b"} {
		req := httptest.NewRequest(http.MethodPost, "/shop.web.v1.WebService/GetPage", nil)
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	lines := logLines(t, &buf)
	if len(lines) != 4 {
		t.Fatalf("%d log lines, want 4:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		if line["msg"] != bfftools.ActivityMessage || line["sha"] != sha || line["procedure"] != "/shop.web.v1.WebService/GetPage" {
			t.Errorf("line %d = %v", i, line)
		}
		if line["level"] != "INFO" {
			t.Errorf("line %d level = %v, want INFO", i, line["level"])
		}
	}
	if _, ok := lines[1]["caller"]; ok {
		t.Errorf("a request without Authorization logged a caller: %v", lines[1])
	}
	a1, a2, b := lines[0]["caller"], lines[2]["caller"], lines[3]["caller"]
	if a1 == nil || a1 != a2 || a1 == b {
		t.Errorf("callers %v, %v, %v; want the first two equal and the third different", a1, a2, b)
	}
	for _, line := range lines {
		if c, _ := line["caller"].(string); strings.Contains(c, "secret") {
			t.Errorf("the caller key holds the token: %v", line)
		}
	}
}

// A very long path is cut in the log, so a caller cannot grow it.
func TestActivityLogBoundsProcedure(t *testing.T) {
	var buf bytes.Buffer
	a := bfftools.NewActivity(sha, bfftools.ActivityOptions{Log: slog.New(slog.NewJSONHandler(&buf, nil))})
	a.Middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/"+strings.Repeat("x", 4096), nil))
	lines := logLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("%d lines", len(lines))
	}
	if p, _ := lines[0]["procedure"].(string); len(p) > bfftools.MaxProcedureLength {
		t.Errorf("procedure logged with %d bytes, want at most %d", len(p), bfftools.MaxProcedureLength)
	}
}

// The consumer's CallerKey tells callers apart, such as by user and
// location, so a refreshed token is the same caller. It is never asked
// about a request without Authorization, which is not tracked.
func TestActivityCallerKey(t *testing.T) {
	var buf bytes.Buffer
	byUser := func(r *http.Request) string {
		if r.Header.Get("Authorization") == "" {
			t.Error("CallerKey was asked about a request without Authorization")
		}
		return r.Header.Get("X-User") + "/" + r.Header.Get("X-Location")
	}
	a := bfftools.NewActivity(sha, bfftools.ActivityOptions{Log: slog.New(slog.NewJSONHandler(&buf, nil)), CallerKey: byUser})
	h := a.Middleware(http.NotFoundHandler())
	for _, c := range []struct{ token, user, location string }{
		{"Bearer t1", "u1", "l1"},
		{"Bearer t2", "u1", "l1"}, // the same user and location, a refreshed token
		{"Bearer t3", "u1", "l2"},
		{"", "u9", "l9"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if c.token != "" {
			req.Header.Set("Authorization", c.token)
		}
		req.Header.Set("X-User", c.user)
		req.Header.Set("X-Location", c.location)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if r := a.Report(); r.Requests != 4 || r.Callers != 2 {
		t.Errorf("report = %+v, want 4 requests from 2 callers", r)
	}
	lines := logLines(t, &buf)
	want := []any{"u1/l1", "u1/l1", "u1/l2", nil}
	for i, line := range lines {
		if line["caller"] != want[i] {
			t.Errorf("line %d caller = %v, want %v", i, line["caller"], want[i])
		}
	}
}

// The default caller key is AuthorizationHash: stable for one token,
// different for another, and never the token.
func TestAuthorizationHash(t *testing.T) {
	req := func(token string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", token)
		return r
	}
	a, again, b := bfftools.AuthorizationHash(req("Bearer a")), bfftools.AuthorizationHash(req("Bearer a")), bfftools.AuthorizationHash(req("Bearer b"))
	if a == "" || a != again || a == b || strings.Contains(a, "Bearer") {
		t.Errorf("AuthorizationHash = %q, %q, %q", a, again, b)
	}
	if got := bfftools.AuthorizationHash(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Errorf("AuthorizationHash without Authorization = %q, want empty", got)
	}
}
