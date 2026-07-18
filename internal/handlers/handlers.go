package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	mux.HandleFunc("GET /api/auth/steam", auth.SteamOpenIDStartHandler)
	mux.HandleFunc("GET /api/auth/steam/callback", auth.SteamOpenIDCallbackHandler)

	// Users (full admin)
	mux.HandleFunc("GET /api/users", apiFullAdmin(ListUsersHandler))
	mux.HandleFunc("POST /api/users", apiFullAdmin(CreateUserHandler))
	mux.HandleFunc("PATCH /api/users/{id}", apiFullAdmin(UpdateUserHandler))
	mux.HandleFunc("DELETE /api/users/{id}", apiFullAdmin(DeleteUserHandler))

	// Host topology (full admin)
	mux.HandleFunc("GET /api/host/cpu-topology", apiFullAdmin(HostCPUTopologyHandler))

	// Settings (full admin)
	mux.HandleFunc("GET /api/settings", apiFullAdmin(GetSettingsHandler))
	mux.HandleFunc("POST /api/settings", apiFullAdmin(SaveSettingsAPIHandler))

	// Steam login (built-in gosteam downloader) — full admin
	mux.HandleFunc("GET /api/steam/status", apiFullAdmin(SteamStatusHandler))
	mux.HandleFunc("GET /api/steam/login/stream", apiFullAdmin(SteamLoginStreamHandler))
	mux.HandleFunc("POST /api/steam/login", apiFullAdmin(SteamLoginHandler))
	mux.HandleFunc("POST /api/steam/login/guard", apiFullAdmin(SteamGuardHandler))
	mux.HandleFunc("POST /api/steam/logout", apiFullAdmin(SteamLogoutHandler))

	// Modsets — read for server admins (pickers); write for mods role
	mux.HandleFunc("GET /api/modsets", apiModsRead(GetModsetsHandler))
	mux.HandleFunc("POST /api/modsets/upload", apiMods(UploadModsetAPIHandler))
	mux.HandleFunc("DELETE /api/modsets/{id}", apiMods(DeleteModsetAPIHandler))
	mux.HandleFunc("DELETE /api/modsets/{id}/mods/{modId}", apiMods(RemoveModFromModsetAPIHandler))

	// Mods — read for server admins; write for mods role
	mux.HandleFunc("GET /api/mods", apiModsRead(GetModsHandler))
	mux.HandleFunc("GET /api/mods/{id}", apiModsRead(GetModHandler))
	mux.HandleFunc("POST /api/mods", apiMods(AddModAPIHandler))
	mux.HandleFunc("POST /api/mods/bulk-delete", apiMods(BulkDeleteModsHandler))
	mux.HandleFunc("DELETE /api/mods/{id}", apiMods(DeleteModAPIHandler))
	mux.HandleFunc("POST /api/mods/check-updates", apiMods(CheckUpdatesAPIHandler))
	mux.HandleFunc("POST /api/mods/{id}/download", apiMods(DownloadModAPIHandler))

	// Download queue — mods role
	mux.HandleFunc("POST /api/downloads/enqueue", apiMods(EnqueueDownloadsHandler))
	mux.HandleFunc("GET /api/downloads/status", apiMods(DownloadStatusHandler))
	mux.HandleFunc("GET /api/downloads/stream", apiMods(DownloadStreamHandler))

	// A3Sync + startup command — mods role
	mux.HandleFunc("POST /api/a3sync/generate", apiMods(GenerateA3SyncAPIHandler))
	mux.HandleFunc("GET /api/startup-command", apiMods(StartupCommandAPIHandler))

	// Branches — read for server admins (name/binary pickers); write for branches role
	mux.HandleFunc("GET /api/branches", apiBranchesRead(GetBranchesHandler))
	mux.HandleFunc("POST /api/branches", apiBranches(CreateBranchHandler))
	mux.HandleFunc("DELETE /api/branches/{id}", apiBranches(DeleteBranchHandler))
	mux.HandleFunc("POST /api/branches/{id}/install", apiBranches(InstallBranchHandler))
	mux.HandleFunc("GET /api/branches/{id}/binaries", apiBranchesRead(ListBranchBinariesHandler))
	mux.HandleFunc("GET /api/branches/install/stream", apiBranches(BranchInstallStreamHandler))
	mux.HandleFunc("GET /api/cdlcs", apiBranchesRead(GetCdlcsHandler))

	// Servers
	mux.HandleFunc("GET /api/servers", apiAuth(GetServersHandler))
	mux.HandleFunc("POST /api/servers", apiFullAdmin(CreateServerHandler))
	mux.HandleFunc("PUT /api/servers/reorder", apiFullAdmin(ReorderServersHandler))
	mux.HandleFunc("GET /api/servers/stream", apiAuth(ServersStreamHandler))
	mux.HandleFunc("GET /api/servers/{id}", apiServerAdmin(GetServerHandler))
	mux.HandleFunc("PUT /api/servers/{id}", apiServerAdmin(UpdateServerHandler))
	mux.HandleFunc("DELETE /api/servers/{id}", apiFullAdmin(DeleteServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/provision", apiServerAdmin(ProvisionServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/start", apiServerAdmin(StartServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/stop", apiServerAdmin(StopServerHandler))
	mux.HandleFunc("POST /api/servers/{id}/restart", apiServerAdmin(RestartServerHandler))
	mux.HandleFunc("GET /api/servers/{id}/status", apiServerAdmin(ServerStatusHandler))
	mux.HandleFunc("GET /api/servers/{id}/launch", apiServerAdmin(ServerLaunchPreviewHandler))

	// Server mods
	mux.HandleFunc("GET /api/servers/{id}/mods", apiServerAdmin(GetServerModsHandler))
	mux.HandleFunc("PUT /api/servers/{id}/mods", apiServerAdmin(PutServerModsHandler))
	mux.HandleFunc("POST /api/localmods/discover", apiAuth(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || !p.CanAccessServers() {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		DiscoverLocalModsHandler(w, r)
	}))

	// Server keys
	mux.HandleFunc("GET /api/servers/{id}/keys", apiServerAdmin(GetServerKeysHandler))
	mux.HandleFunc("POST /api/servers/{id}/keys/rescan", apiServerAdmin(RescanServerKeysHandler))
	mux.HandleFunc("POST /api/servers/{id}/keys", apiServerAdmin(UploadServerKeyHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/keys/{keyId}", apiServerAdmin(DeleteServerKeyHandler))

	// Server config
	mux.HandleFunc("GET /api/servers/{id}/config/{name}", apiServerAdmin(GetServerConfigHandler))
	mux.HandleFunc("PUT /api/servers/{id}/config/{name}", apiServerAdmin(PutServerConfigHandler))
	mux.HandleFunc("GET /api/servers/{id}/profile/files", apiServerAdmin(ListProfileFilesHandler))
	mux.HandleFunc("GET /api/servers/{id}/profile/{filename}", apiServerAdmin(GetProfileFileHandler))
	mux.HandleFunc("PUT /api/servers/{id}/profile/{filename}", apiServerAdmin(PutProfileFileHandler))

	// Server logs
	mux.HandleFunc("GET /api/servers/{id}/logs", apiServerAdmin(ListServerLogsHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/stream", apiServerAdmin(ServerLogStreamHandler))
	mux.HandleFunc("POST /api/servers/{id}/logs/delete", apiServerAdmin(DeleteServerLogsHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/{filename}/download", apiServerAdmin(DownloadServerLogHandler))
	mux.HandleFunc("GET /api/servers/{id}/logs/{filename}", apiServerAdmin(GetServerLogHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/logs/{filename}", apiServerAdmin(DeleteServerLogHandler))

	// Headless clients
	mux.HandleFunc("GET /api/servers/{id}/headless", apiServerAdmin(GetHeadlessHandler))
	mux.HandleFunc("PUT /api/servers/{id}/headless", apiServerAdmin(PutHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/start", apiServerAdmin(StartHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/stop", apiServerAdmin(StopHeadlessHandler))
	mux.HandleFunc("POST /api/servers/{id}/headless/{slot}/restart", apiServerAdmin(RestartHeadlessHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs", apiServerAdmin(ListHeadlessLogsHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/stream", apiServerAdmin(HeadlessLogStreamHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/{filename}/download", apiServerAdmin(DownloadHeadlessLogHandler))
	mux.HandleFunc("GET /api/servers/{id}/headless/{slot}/logs/{filename}", apiServerAdmin(GetHeadlessLogHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/headless/{slot}/logs/{filename}", apiServerAdmin(DeleteHeadlessLogHandler))

	// Missions
	mux.HandleFunc("GET /api/servers/{id}/missions", apiServerAdmin(ListMissionsHandler))
	mux.HandleFunc("POST /api/servers/{id}/missions/upload", apiServerAdmin(UploadMissionHandler))
	mux.HandleFunc("POST /api/servers/{id}/missions/delete", apiServerAdmin(DeleteMissionsHandler))
	mux.HandleFunc("PUT /api/servers/{id}/missions/http-serve", apiServerAdmin(PutMissionHttpServeHandler))
	mux.HandleFunc("GET /api/servers/{id}/missions/{name}/download", apiServerAdmin(DownloadMissionHandler))
	mux.HandleFunc("DELETE /api/servers/{id}/missions/{name}", apiServerAdmin(DeleteMissionHandler))

	return mux
}

func attachPrincipal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.LoadPrincipalAndRenew(w, r)
		if p == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	}
}

// apiAuth requires any valid session and attaches Principal to the request context.
func apiAuth(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(next)
}

func apiFullAdmin(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || !p.IsFullAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

func apiMods(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || !p.CanManageMods {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

// apiModsRead allows mod catalog reads for mod managers and server admins.
func apiModsRead(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || (!p.CanManageMods && !p.CanAccessServers()) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

func apiBranches(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || !p.CanManageBranches {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

// apiBranchesRead allows branch catalog reads for branch managers and server admins.
func apiBranchesRead(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p == nil || (!p.CanManageBranches && !p.CanAccessServers()) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

// apiServerAdmin requires access to the server identified by path {id}.
func apiServerAdmin(next http.HandlerFunc) http.HandlerFunc {
	return attachPrincipal(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid server id")
			return
		}
		if p == nil || !p.HasServerAccess(id) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
