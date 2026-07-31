package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
	})
}

func TestCORSNoAllowlistIsPassthrough(t *testing.T) {
	h := CORS(okHandler(), []string{"", "  "})

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Origin", "vscode-webview://abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header without an allowlist, got %q", got)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "body" {
		t.Fatalf("expected the request to pass through, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestCORSMatching(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		origin  string
		want    bool
	}{
		{"scheme wildcard", []string{"vscode-webview://*"}, "vscode-webview://8f2c-4a1b", true},
		{"scheme wildcard rejects other scheme", []string{"vscode-webview://*"}, "http://evil.example", false},
		{"exact match", []string{"http://127.0.0.1:5173"}, "http://127.0.0.1:5173", true},
		{"exact match is port sensitive", []string{"http://127.0.0.1:5173"}, "http://127.0.0.1:5174", false},
		{"trailing slash in allowlist entry", []string{"http://127.0.0.1:5173/"}, "http://127.0.0.1:5173", true},
		{"comma separated list", []string{"http://a.example,vscode-webview://*"}, "vscode-webview://x", true},
		{"full wildcard", []string{"*"}, "http://anything.example", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := CORS(okHandler(), tc.allowed)

			req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get("Access-Control-Allow-Origin")
			if tc.want && got != tc.origin {
				t.Fatalf("expected the origin echoed back, got %q", got)
			}
			if !tc.want && got != "" {
				t.Fatalf("expected no CORS header for a disallowed origin, got %q", got)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("expected the request to reach the handler, got %d", rec.Code)
			}
		})
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	reached := false
	h := CORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}), []string{"vscode-webview://*"})

	req := httptest.NewRequest(http.MethodOptions, "/api/events", nil)
	req.Header.Set("Origin", "vscode-webview://abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for a preflight, got %d", rec.Code)
	}
	if reached {
		t.Fatal("preflight should not reach the wrapped handler")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("preflight response is missing Access-Control-Allow-Headers")
	}
}

func TestCORSPreflightFromDisallowedOriginIsNotShortCircuited(t *testing.T) {
	h := CORS(okHandler(), []string{"vscode-webview://*"})

	req := httptest.NewRequest(http.MethodOptions, "/api/events", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Falls through to the wrapped handler, which answers without any CORS
	// header — the browser then refuses the real request, as it should.
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin must not receive CORS headers")
	}
}

func TestCORSSetsVaryOrigin(t *testing.T) {
	h := CORS(okHandler(), []string{"vscode-webview://*"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "vscode-webview://abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("expected Vary: Origin, got %q", rec.Header().Get("Vary"))
	}
}
