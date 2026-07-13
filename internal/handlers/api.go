package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/auth"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
	"arma3modmanager/internal/parser"
	"arma3modmanager/internal/queue"
	"arma3modmanager/internal/utils"
	"arma3modmanager/internal/workshop"
)

// --- Auth ---

func LoginAPIHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !auth.ValidatePassword(body.Password) {
		writeError(w, http.StatusUnauthorized, "Invalid password")
		return
	}
	if err := auth.CreateSession(w); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func LogoutAPIHandler(w http.ResponseWriter, r *http.Request) {
	auth.DestroySession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func SessionHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": auth.ValidateSession(r)})
}

// --- Settings ---

func GetSettingsHandler(w http.ResponseWriter, r *http.Request) {
	config, _ := database.GetConfig()
	resp := map[string]interface{}{
		"username":               "",
		"apiKey":                 "",
		"hasPassword":            false,
		"downloader":             depot.DownloaderGoSteam,
		"steamCmdPath":           "",
		"depotDownloaderPath":    "",
		"maxConcurrentDownloads": 4,
	}
	if config != nil {
		resp["username"] = config.Username
		resp["apiKey"] = config.ApiKey
		resp["hasPassword"] = config.Password != ""
		if config.Downloader != "" {
			resp["downloader"] = config.Downloader
		}
		resp["steamCmdPath"] = config.SteamCmdPath
		resp["depotDownloaderPath"] = config.DepotDownloaderPath
		if config.MaxConcurrentDownloads > 0 {
			resp["maxConcurrentDownloads"] = config.MaxConcurrentDownloads
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func SaveSettingsAPIHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username               string `json:"username"`
		Password               string `json:"password"`
		ApiKey                 string `json:"apiKey"`
		Downloader             string `json:"downloader"`
		SteamCmdPath           string `json:"steamCmdPath"`
		DepotDownloaderPath    string `json:"depotDownloaderPath"`
		MaxConcurrentDownloads int    `json:"maxConcurrentDownloads"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// An empty password means "keep the existing one".
	password := body.Password
	if password == "" {
		if existing, _ := database.GetConfig(); existing != nil {
			password = existing.Password
		}
	}

	// Coerce the downloader to a known value (default to the built-in gosteam).
	downloader := body.Downloader
	switch downloader {
	case depot.DownloaderSteamCmd, depot.DownloaderDepot, depot.DownloaderGoSteam:
	default:
		downloader = depot.DownloaderGoSteam
	}

	if err := database.SaveConfig(body.Username, password, body.ApiKey, downloader, body.SteamCmdPath, body.DepotDownloaderPath, body.MaxConcurrentDownloads); err != nil {
		log.Printf("Error saving config: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Modsets ---

func GetModsetsHandler(w http.ResponseWriter, r *http.Request) {
	modsets, err := database.GetModsets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch modsets")
		return
	}
	if modsets == nil {
		modsets = []database.Modset{}
	}
	writeJSON(w, http.StatusOK, modsets)
}

func UploadModsetAPIHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}

	file, _, err := r.FormFile("modset")
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	defer file.Close()

	modsetName, mods, err := parser.ParseModsetHTML(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse modset HTML: "+err.Error())
		return
	}

	modsetID, err := database.CreateModset(modsetName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create modset: "+err.Error())
		return
	}

	for _, mod := range mods {
		if err := database.AddMod(int(modsetID), mod.WorkshopID, mod.Name); err != nil {
			log.Printf("Failed to add mod %s: %v", mod.Name, err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modsetId": modsetID,
		"name":     modsetName,
		"count":    len(mods),
	})
}

func DeleteModsetAPIHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid modset id")
		return
	}
	if err := database.DeleteModset(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete modset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Mods ---

func GetModsHandler(w http.ResponseWriter, r *http.Request) {
	modsetIDStr := r.URL.Query().Get("modset")
	var mods []database.Mod
	var err error

	if modsetIDStr != "" {
		modsetID, _ := strconv.Atoi(modsetIDStr)
		mods, err = database.GetMods(modsetID)
	} else {
		mods, err = database.GetAllMods()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mods")
		return
	}
	if mods == nil {
		mods = []database.Mod{}
	}
	writeJSON(w, http.StatusOK, mods)
}

func AddModAPIHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkshopID string `json:"workshopId"`
		ModsetID   *int   `json:"modsetId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.WorkshopID == "" {
		writeError(w, http.StatusBadRequest, "workshop id required")
		return
	}

	config, err := database.GetConfig()
	if err != nil || config == nil {
		writeError(w, http.StatusBadRequest, "Steam API key not configured")
		return
	}

	modName, err := workshop.GetModName(body.WorkshopID, config.ApiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mod info: "+err.Error())
		return
	}

	if body.ModsetID != nil {
		if err := database.AddMod(*body.ModsetID, body.WorkshopID, modName); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add mod")
			return
		}
	} else {
		if err := database.CreateMod(body.WorkshopID, modName); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create mod")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"workshopId": body.WorkshopID, "name": modName})
}

func deleteModFilesAndRecord(id int) error {
	mod, err := database.GetMod(id)
	if err != nil {
		return err
	}

	if mod.LocalPath != "" {
		if err := utils.RemoveModFromAddons(mod.LocalPath); err != nil {
			log.Printf("[ERROR] Failed to remove mod from addons: %v", err)
		}
		if err := os.RemoveAll(mod.LocalPath); err != nil {
			log.Printf("[ERROR] Failed to delete mod files: %v", err)
		}
	}

	return database.DeleteMod(id)
}

func DeleteModAPIHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid mod id")
		return
	}

	if err := deleteModFilesAndRecord(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete mod")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func BulkDeleteModsHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids required")
		return
	}

	deleted := 0
	var failed []int
	for _, id := range body.IDs {
		if err := deleteModFilesAndRecord(id); err != nil {
			log.Printf("[ERROR] Bulk delete failed for mod %d: %v", id, err)
			failed = append(failed, id)
			continue
		}
		deleted++
	}
	if failed == nil {
		failed = []int{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

func RemoveModFromModsetAPIHandler(w http.ResponseWriter, r *http.Request) {
	modsetID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid modset id")
		return
	}
	modID, err := strconv.Atoi(r.PathValue("modId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid mod id")
		return
	}

	if err := database.RemoveModFromModset(modsetID, modID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove mod from modset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func CheckUpdatesAPIHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Modset *int `json:"modset"`
	}
	// Empty body is fine (check all mods).
	_ = json.NewDecoder(r.Body).Decode(&body)

	var mods []database.Mod
	var err error
	if body.Modset != nil {
		mods, err = database.GetMods(*body.Modset)
	} else if q := r.URL.Query().Get("modset"); q != "" {
		modsetID, _ := strconv.Atoi(q)
		mods, err = database.GetMods(modsetID)
	} else {
		mods, err = database.GetAllMods()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mods")
		return
	}

	config, err := database.GetConfig()
	if err != nil || config == nil || config.ApiKey == "" {
		writeError(w, http.StatusBadRequest, "Steam API key not configured. Please set it in settings.")
		return
	}

	updates := make(map[int]bool)

	var workshopIDs []string
	modMap := make(map[string]database.Mod)
	for _, mod := range mods {
		workshopIDs = append(workshopIDs, mod.WorkshopID)
		modMap[mod.WorkshopID] = mod
	}

	details, err := workshop.GetModDetails(workshopIDs, config.ApiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to check updates: %v", err))
		return
	}

	for _, detail := range details {
		if detail.Result != 1 {
			log.Printf("Failed to get details for workshop ID %s, result code: %d", detail.PublishedFileID, detail.Result)
			continue
		}

		mod, exists := modMap[detail.PublishedFileID]
		if !exists {
			continue
		}

		lastUpdate := time.Unix(detail.TimeUpdated, 0)
		updateAvailable := false
		if mod.Downloaded && !mod.LastUpdated.IsZero() {
			updateAvailable = lastUpdate.After(mod.LastUpdated)
		} else if !mod.Downloaded {
			updateAvailable = true
		}

		updates[mod.ID] = updateAvailable
		database.UpdateModChecked(mod.ID, updateAvailable)
	}

	writeJSON(w, http.StatusOK, updates)
}

// --- Downloads (queue-backed) ---

func DownloadModAPIHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid mod id")
		return
	}

	mod, err := database.GetMod(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}

	queue.Default.Enqueue([]database.Mod{*mod})
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func EnqueueDownloadsHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope  string `json:"scope"` // "all", "outdated", "missing", "ids"
		Modset *int   `json:"modset"`
		IDs    []int  `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Explicit ID list: enqueue every matching mod (download / redownload).
	if len(body.IDs) > 0 {
		targets, err := database.GetModsByIDs(body.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to fetch mods")
			return
		}
		added := queue.Default.Enqueue(targets)
		writeJSON(w, http.StatusOK, map[string]int{"enqueued": added})
		return
	}

	var mods []database.Mod
	var err error
	if body.Modset != nil {
		mods, err = database.GetMods(*body.Modset)
	} else {
		mods, err = database.GetAllMods()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mods")
		return
	}

	var targets []database.Mod
	for _, m := range mods {
		switch body.Scope {
		case "missing":
			if !m.Downloaded {
				targets = append(targets, m)
			}
		case "outdated":
			if m.Downloaded && m.UpdateAvailable {
				targets = append(targets, m)
			}
		default: // "all": everything missing or outdated
			if !m.Downloaded || m.UpdateAvailable {
				targets = append(targets, m)
			}
		}
	}

	added := queue.Default.Enqueue(targets)
	writeJSON(w, http.StatusOK, map[string]int{"enqueued": added})
}

func DownloadStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, queue.Default.Snapshot())
}

func DownloadStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch := queue.Default.Subscribe()
	defer queue.Default.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case snap, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(snap)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- A3Sync + startup command ---

func GenerateA3SyncAPIHandler(w http.ResponseWriter, r *http.Request) {
	allMods, err := database.GetAllMods()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mods")
		return
	}

	var modsForSync []struct{ LocalPath, Name string }
	for _, mod := range allMods {
		if mod.Downloaded && mod.LocalPath != "" {
			modsForSync = append(modsForSync, struct{ LocalPath, Name string }{
				LocalPath: mod.LocalPath,
				Name:      mod.Name,
			})
		}
	}

	if err := utils.GenerateA3SyncFolder(modsForSync); err != nil {
		log.Printf("Failed to generate a3sync folder: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to generate a3sync folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func StartupCommandAPIHandler(w http.ResponseWriter, r *http.Request) {
	modsetIDStr := r.URL.Query().Get("modset")
	if modsetIDStr == "" {
		writeError(w, http.StatusBadRequest, "modset id required")
		return
	}
	modsetID, _ := strconv.Atoi(modsetIDStr)

	mods, err := database.GetMods(modsetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch mods")
		return
	}

	var modPaths []string
	for _, mod := range mods {
		if mod.Downloaded && mod.LocalPath != "" {
			modPaths = append(modPaths, mod.LocalPath)
		}
	}

	if err := utils.ExtractKeys(modPaths, KeysDirectory); err != nil {
		log.Printf("Failed to extract keys: %v", err)
	}

	var modParams []string
	for _, path := range modPaths {
		modParams = append(modParams, "mods/"+filepath.Base(path))
	}
	command := fmt.Sprintf("mods=\"%s\"", strings.Join(modParams, ";"))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"command":  command,
		"keysPath": KeysDirectory,
		"modPaths": modPaths,
	})
}
