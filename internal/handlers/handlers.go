package handlers

import (
	"encoding/json"
	"net/http"

	"arma3modmanager/internal/auth"
)

const (
	ModsDirectory = "./mods"
	KeysDirectory = "./keys"
)

// APIHandler builds the JSON REST router mounted at /api.
func APIHandler() http.Handler {
	mux := http.NewServeMux()

	// Public (no session required)
	mux.HandleFunc("POST /api/login", LoginAPIHandler)
	mux.HandleFunc("POST /api/logout", LogoutAPIHandler)
	mux.HandleFunc("GET /api/session", SessionHandler)

	// Settings
	mux.HandleFunc("GET /api/settings", apiAuth(GetSettingsHandler))
	mux.HandleFunc("POST /api/settings", apiAuth(SaveSettingsAPIHandler))

	// Modsets
	mux.HandleFunc("GET /api/modsets", apiAuth(GetModsetsHandler))
	mux.HandleFunc("POST /api/modsets/upload", apiAuth(UploadModsetAPIHandler))
	mux.HandleFunc("DELETE /api/modsets/{id}", apiAuth(DeleteModsetAPIHandler))

	// Mods
	mux.HandleFunc("GET /api/mods", apiAuth(GetModsHandler))
	mux.HandleFunc("POST /api/mods", apiAuth(AddModAPIHandler))
	mux.HandleFunc("DELETE /api/mods/{id}", apiAuth(DeleteModAPIHandler))
	mux.HandleFunc("POST /api/mods/check-updates", apiAuth(CheckUpdatesAPIHandler))
	mux.HandleFunc("POST /api/mods/{id}/download", apiAuth(DownloadModAPIHandler))

	// Download queue
	mux.HandleFunc("POST /api/downloads/enqueue", apiAuth(EnqueueDownloadsHandler))
	mux.HandleFunc("GET /api/downloads/status", apiAuth(DownloadStatusHandler))
	mux.HandleFunc("GET /api/downloads/stream", apiAuth(DownloadStreamHandler))

	// A3Sync + startup command
	mux.HandleFunc("POST /api/a3sync/generate", apiAuth(GenerateA3SyncAPIHandler))
	mux.HandleFunc("GET /api/startup-command", apiAuth(StartupCommandAPIHandler))

	return mux
}

// apiAuth wraps a handler so unauthenticated requests get a JSON 401 instead of
// a redirect (the SPA reacts to 401 by routing to the login page).
func apiAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.ValidateSession(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
