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

	// Steam login (built-in gosteam downloader)
	mux.HandleFunc("GET /api/steam/status", apiAuth(SteamStatusHandler))
	mux.HandleFunc("GET /api/steam/login/stream", apiAuth(SteamLoginStreamHandler))
	mux.HandleFunc("POST /api/steam/login", apiAuth(SteamLoginHandler))
	mux.HandleFunc("POST /api/steam/login/guard", apiAuth(SteamGuardHandler))
	mux.HandleFunc("POST /api/steam/logout", apiAuth(SteamLogoutHandler))

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

	// Branches
	mux.HandleFunc("GET /api/branches", apiAuth(GetBranchesHandler))
	mux.HandleFunc("POST /api/branches", apiAuth(CreateBranchHandler))
	mux.HandleFunc("DELETE /api/branches/{id}", apiAuth(DeleteBranchHandler))
	mux.HandleFunc("POST /api/branches/{id}/install", apiAuth(InstallBranchHandler))
	mux.HandleFunc("GET /api/branches/{id}/binaries", apiAuth(ListBranchBinariesHandler))
	mux.HandleFunc("GET /api/branches/install/stream", apiAuth(BranchInstallStreamHandler))

	// Servers
	mux.HandleFunc("GET /api/servers", apiAuth(GetServersHandler))
	mux.HandleFunc("POST /api/servers", apiAuth(CreateServerHandler))
	mux.HandleFunc("GET /api/servers/stream", apiAuth(ServersStreamHandler))
	mux.HandleFunc("GET /api/servers/{id}", apiAuth(GetServerHandler))
	mux.HandleFunc("PUT /api/servers/{id}", apiAuth(UpdateServerHandler))
	mux.HandleFunc("DELETE /api/servers/{id}", apiAuth(DeleteServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/provision", apiAuth(ProvisionServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/start", apiAuth(StartServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/stop", apiAuth(StopServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/restart", apiAuth(RestartServerHandler))
	mux.HandleFunc("GET /api/servers/{id}/status", apiAuth(ServerStatusHandler))
	mux.HandleFunc("GET /api/servers/{id}/launch", apiAuth(ServerLaunchPreviewHandler))

	// Server mods
	mux.HandleFunc("GET /api/servers/{id}/mods", apiAuth(GetServerModsHandler))
	mux.HandleFunc("PUT /api/servers/{id}/mods", apiAuth(PutServerModsHandler))
	mux.HandleFunc("POST /api/localmods/discover", apiAuth(DiscoverLocalModsHandler))

	// Server keys
	mux.HandleFunc("GET /api/servers/{id}/keys", apiAuth(GetServerKeysHandler))
	mux.HandleFunc("POST /api/servers/{id}/keys/rescan", apiAuth(RescanServerKeysHandler))
	mux.HandleFunc("POST /api/servers/{id}/keys", apiAuth(UploadServerKeyHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/keys/{keyId}", apiAuth(DeleteServerKeyHandler))

	// Server config
	mux.HandleFunc("GET /api/servers/{id}/config/{name}", apiAuth(GetServerConfigHandler))
	mux.HandleFunc("PUT /api/servers/{id}/config/{name}", apiAuth(PutServerConfigHandler))
	mux.HandleFunc("GET /api/servers/{id}/profile/files", apiAuth(ListProfileFilesHandler))
	mux.HandleFunc("GET /api/servers/{id}/profile/{filename}", apiAuth(GetProfileFileHandler))
	mux.HandleFunc("PUT /api/servers/{id}/profile/{filename}", apiAuth(PutProfileFileHandler))

	// Server logs
	mux.HandleFunc("GET /api/servers/{id}/logs", apiAuth(ListServerLogsHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/stream", apiAuth(ServerLogStreamHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/{filename}/download", apiAuth(DownloadServerLogHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/{filename}", apiAuth(GetServerLogHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/logs/{filename}", apiAuth(DeleteServerLogHandler))

	// Headless clients
	mux.HandleFunc("GET /api/servers/{id}/headless", apiAuth(GetHeadlessHandler))
	mux.HandleFunc("PUT /api/servers/{id}/headless", apiAuth(PutHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/start", apiAuth(StartHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/stop", apiAuth(StopHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/restart", apiAuth(RestartHeadlessHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs", apiAuth(ListHeadlessLogsHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/stream", apiAuth(HeadlessLogStreamHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/{filename}/download", apiAuth(DownloadHeadlessLogHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/{filename}", apiAuth(GetHeadlessLogHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/headless/{slot}/logs/{filename}", apiAuth(DeleteHeadlessLogHandler))

	// Missions
	mux.HandleFunc("GET /api/servers/{id}/missions", apiAuth(ListMissionsHandler))
	mux.HandleFunc("POST /api/servers/{id}/missions/upload", apiAuth(UploadMissionHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/missions/{name}", apiAuth(DeleteMissionHandler))

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
