package backup

import (
	"encoding/json"
	"net/http"
	"strings"
)

// routes builds the internal HTTP multiplexer.
// All paths are relative to the configured BasePath.
func (m *Manager) routes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the base path prefix so sub-handlers see clean paths.
		path := strings.TrimPrefix(r.URL.Path, m.basePath)
		if path == "" {
			path = "/"
		}

		switch {
		// --- backup operations ---
		case r.Method == http.MethodGet && path == "/status":
			m.handleStatus(w, r)
		case r.Method == http.MethodPost && path == "/trigger":
			m.handleTrigger(w, r)
		case r.Method == http.MethodGet && path == "/history":
			m.handleListHistory(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/history/") && !strings.HasSuffix(path, "/download"):
			id := strings.TrimPrefix(path, "/history/")
			m.handleDeleteHistory(w, r, id)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/download"):
			id := strings.TrimPrefix(strings.TrimSuffix(path, "/download"), "/history/")
			m.handleDownloadHistory(w, r, id)

		// --- settings ---
		case r.Method == http.MethodGet && path == "/settings":
			m.handleGetSettings(w, r)
		case r.Method == http.MethodPut && path == "/settings":
			m.handlePutSettings(w, r)
		case r.Method == http.MethodPost && path == "/test-connection":
			m.handleTestConnection(w, r)

		// --- folder browsing ---
		case r.Method == http.MethodGet && path == "/folders":
			m.handleListFolders(w, r)
		case r.Method == http.MethodPost && path == "/folders":
			m.handleCreateFolder(w, r)

		// --- OAuth (only if provider supports it) ---
		case r.Method == http.MethodGet && path == "/oauth/start":
			m.handleOAuthStart(w, r)
		case r.Method == http.MethodGet && path == "/oauth/callback":
			m.handleOAuthCallback(w, r)
		case r.Method == http.MethodDelete && path == "/oauth/disconnect":
			m.handleOAuthDisconnect(w, r)

		default:
			http.NotFound(w, r)
		}
	})
}

// PublicHandler returns an http.Handler that serves ONLY the OAuth redirect
// endpoints (/oauth/start and /oauth/callback). These must be publicly accessible
// because the browser navigates to /oauth/start without an Authorization header,
// and the OAuth provider redirects to /oauth/callback with no auth header at all.
//
// Mount this on a public (unauthenticated) route before any auth middleware:
//
//	mux.PathPrefix("/api/admin/backup/oauth").
//	    Handler(http.StripPrefix("/api/admin/backup", m.PublicHandler()))
func (m *Manager) PublicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, m.basePath)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		switch {
		case r.Method == http.MethodGet && path == "/oauth/start":
			m.handleOAuthStart(w, r)
		case r.Method == http.MethodGet && path == "/oauth/callback":
			m.handleOAuthCallback(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
