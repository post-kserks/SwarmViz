package server

import (
	"net/http"
	"strings"
)

// CORS wraps a handler with an origin allowlist.
//
// The default deployment needs none of this: the UI is served by the same
// binary, so every request is same-origin. A host that embeds the UI in its
// own document is the exception — a VS Code webview runs on an opaque
// vscode-webview:// origin, so its fetch of /api/projects is cross-origin and
// the browser drops the response unless the server opts in.
//
// The allowlist is explicit on purpose. Answering every origin with a wildcard
// would let any page the user happens to have open read the diff stream of
// every repository this process watches.
//
// Entries are matched in three forms:
//
//	*                     — any origin (only sensible for throwaway debugging)
//	vscode-webview://*    — any origin with that scheme
//	http://127.0.0.1:5173 — exact match
//
// WebSocket upgrades are deliberately untouched: they are not subject to CORS,
// and the upgrader has its own origin check.
func CORS(next http.Handler, allowed []string) http.Handler {
	patterns := normalizeOrigins(allowed)
	if len(patterns) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(origin, patterns) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			// The allowlist can answer differently per origin, so shared
			// caches must not reuse one origin's response for another.
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// normalizeOrigins splits comma-separated entries, trims them and drops the
// empties, so callers can hand over raw flag values.
func normalizeOrigins(allowed []string) []string {
	var out []string
	for _, entry := range allowed {
		for _, part := range strings.Split(entry, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, strings.TrimSuffix(part, "/"))
			}
		}
	}
	return out
}

func originAllowed(origin string, patterns []string) bool {
	for _, pattern := range patterns {
		switch {
		case pattern == "*":
			return true
		case strings.HasSuffix(pattern, "://*"):
			if strings.HasPrefix(origin, strings.TrimSuffix(pattern, "*")) {
				return true
			}
		case strings.EqualFold(pattern, origin):
			return true
		}
	}
	return false
}
