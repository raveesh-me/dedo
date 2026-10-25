package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	known := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer good" }
	tests := []struct {
		access Access
		token  string
		want   int
	}{
		{"", "", http.StatusOK},
		{AccessPublic, "", http.StatusOK},
		{AccessAuthenticated, "", http.StatusUnauthorized},
		{AccessAuthenticated, "bad", http.StatusUnauthorized},
		{AccessAuthenticated, "good", http.StatusOK},
	}
	for _, tt := range tests {
		h, err := Guard(ok, tt.access, known)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tt.token != "" {
			req.Header.Set("Authorization", "Bearer "+tt.token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("access %q, token %q: status %d, want %d", tt.access, tt.token, rec.Code, tt.want)
		}
		if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("access %q, token %q: no WWW-Authenticate challenge", tt.access, tt.token)
		}
	}
}

// A mistyped setting stops the service instead of serving it publicly.
func TestGuardRejectsUnknownAccess(t *testing.T) {
	if _, err := Guard(http.NotFoundHandler(), "authenticate", nil); err == nil || !strings.Contains(err.Error(), "unknown access") {
		t.Errorf("err = %v", err)
	}
	if _, err := Guard(http.NotFoundHandler(), AccessAuthenticated, nil); err == nil {
		t.Error("AccessAuthenticated without a way to authenticate was accepted")
	}
}
