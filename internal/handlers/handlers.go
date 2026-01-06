package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"arma3modmanager/internal/auth"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
	"arma3modmanager/internal/parser"
	"arma3modmanager/internal/utils"
	"arma3modmanager/internal/workshop"
)

const ModsDirectory = "./mods"
const KeysDirectory = "./keys"

type PageData struct {
	Config  *database.Config
	Modsets []database.Modset
	Mods    []database.Mod
	Error   string
	Success string
}

// Progress tracking for active downloads
var (
	activeDownloads = make(map[int]chan depot.ProgressUpdate)
	downloadsMutex  sync.RWMutex
)

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))

	config, _ := database.GetConfig()
	modsets, _ := database.GetModsets()

	data := PageData{
		Config:  config,
		Modsets: modsets,
	}

	tmpl.Execute(w, data)
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/settings.html"))

	config, _ := database.GetConfig()

	data := PageData{
		Config: config,
	}

	tmpl.Execute(w, data)
}

func SaveSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if err := database.SaveConfig(username, password); err != nil {
		log.Println("Error saving config:", err)
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings?success=1", http.StatusSeeOther)
}

func ModsetsHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/modsets.html"))

	modsets, _ := database.GetModsets()

	data := PageData{
		Modsets: modsets,
	}

	tmpl.Execute(w, data)
}

func UploadModsetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/modsets", http.StatusSeeOther)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB max
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("modset")
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Parse the HTML file
	modsetName, mods, err := parser.ParseModsetHTML(file)
	if err != nil {
		http.Error(w, "Failed to parse modset HTML: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Create modset in database
	modsetID, err := database.CreateModset(modsetName)
	if err != nil {
		http.Error(w, "Failed to create modset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add mods to database
	for _, mod := range mods {
		if err := database.AddMod(int(modsetID), mod.WorkshopID, mod.Name); err != nil {
			log.Printf("Failed to add mod %s: %v\n", mod.Name, err)
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/mods?modset=%d", modsetID), http.StatusSeeOther)
}

func DeleteModsetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/modsets", http.StatusSeeOther)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid modset ID", http.StatusBadRequest)
		return
	}

	if err := database.DeleteModset(id); err != nil {
		http.Error(w, "Failed to delete modset", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/modsets", http.StatusSeeOther)
}

func AddModHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workshopID := r.FormValue("workshop_id")
	modsetIDStr := r.FormValue("modset_id")

	if workshopID == "" {
		http.Error(w, "Workshop ID required", http.StatusBadRequest)
		return
	}

	// Fetch mod name from workshop page
	modName, err := workshop.GetModName(workshopID)
	if err != nil {
		log.Printf("Failed to fetch mod name for %s: %v\n", workshopID, err)
		http.Error(w, fmt.Sprintf("Failed to fetch mod info: %v", err), http.StatusInternalServerError)
		return
	}

	// If modset ID is provided, add to that modset, otherwise just create the mod
	if modsetIDStr != "" {
		modsetID, err := strconv.Atoi(modsetIDStr)
		if err != nil {
			http.Error(w, "Invalid modset ID", http.StatusBadRequest)
			return
		}

		// Add mod to database and link to modset
		if err := database.AddMod(modsetID, workshopID, modName); err != nil {
			log.Printf("Failed to add mod: %v\n", err)
			http.Error(w, "Failed to add mod", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, fmt.Sprintf("/mods?modset=%d", modsetID), http.StatusSeeOther)
	} else {
		// Just create the mod without linking to a modset
		if err := database.CreateMod(workshopID, modName); err != nil {
			log.Printf("Failed to create mod: %v\n", err)
			http.Error(w, "Failed to create mod", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/mods", http.StatusSeeOther)
	}
}

func ModsHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/mods.html"))

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
		log.Println("Error fetching mods:", err)
	}

	modsets, _ := database.GetModsets()

	data := PageData{
		Mods:    mods,
		Modsets: modsets,
	}

	tmpl.Execute(w, data)
}

func GenerateA3SyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all downloaded mods
	allMods, err := database.GetAllMods()
	if err != nil {
		http.Error(w, "Failed to fetch mods", http.StatusInternalServerError)
		return
	}

	// Filter downloaded mods and prepare data for a3sync generation
	type ModData struct {
		LocalPath string
		Name      string
	}
	var downloadedMods []ModData
	for _, mod := range allMods {
		if mod.Downloaded && mod.LocalPath != "" {
			downloadedMods = append(downloadedMods, ModData{
				LocalPath: mod.LocalPath,
				Name:      mod.Name,
			})
		}
	}

	// Generate a3sync folder
	var modsForSync []struct{ LocalPath, Name string }
	for _, mod := range downloadedMods {
		modsForSync = append(modsForSync, struct{ LocalPath, Name string }{
			LocalPath: mod.LocalPath,
			Name:      mod.Name,
		})
	}

	if err := utils.GenerateA3SyncFolder(modsForSync); err != nil {
		log.Printf("Failed to generate a3sync folder: %v\n", err)
		http.Error(w, "Failed to generate a3sync folder", http.StatusInternalServerError)
		return
	}

	// Redirect back to mods page
	http.Redirect(w, r, "/mods", http.StatusSeeOther)
}

func CheckUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	mods, err := database.GetAllMods()
	if err != nil {
		http.Error(w, "Failed to fetch mods", http.StatusInternalServerError)
		return
	}

	updates := make(map[int]bool)

	// Rate limit: 1 request per 2 seconds to avoid Steam throttling
	for i, mod := range mods {
		// Add 2 second delay between requests (except for first one)
		if i > 0 {
			time.Sleep(2 * time.Second)
		}

		lastUpdate, err := workshop.CheckUpdate(mod.WorkshopID)
		if err != nil {
			log.Printf("Failed to check update for %s: %v\n", mod.Name, err)
			continue
		}

		// If mod is downloaded, check if update is newer
		updateAvailable := false
		if mod.Downloaded && !mod.LastUpdated.IsZero() {
			updateAvailable = lastUpdate.After(mod.LastUpdated)
		} else if !mod.Downloaded {
			updateAvailable = true
		}

		updates[mod.ID] = updateAvailable
		database.UpdateModChecked(mod.ID, updateAvailable)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updates)
}

func DownloadModHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid mod ID", http.StatusBadRequest)
		return
	}

	// Get config
	config, err := database.GetConfig()
	if err != nil || config == nil {
		http.Error(w, "Steam credentials not configured", http.StatusBadRequest)
		return
	}

	// Get mod info
	mods, err := database.GetAllMods()
	if err != nil {
		http.Error(w, "Failed to fetch mods", http.StatusInternalServerError)
		return
	}

	var targetMod *database.Mod
	for _, mod := range mods {
		if mod.ID == id {
			targetMod = &mod
			break
		}
	}

	if targetMod == nil {
		http.Error(w, "Mod not found", http.StatusNotFound)
		return
	}

	// Create progress channel
	log.Printf("[DEBUG] Creating progress channel for mod ID: %d (workshop: %s)", id, targetMod.WorkshopID)
	progressChan := make(chan depot.ProgressUpdate, 100)

	// Register the download
	downloadsMutex.Lock()
	activeDownloads[id] = progressChan
	log.Printf("[DEBUG] Registered download in activeDownloads map, mod ID: %d", id)
	downloadsMutex.Unlock()

	// Start download in a goroutine
	go func() {
		log.Printf("[DEBUG] Download goroutine started for mod ID: %d", id)
		defer func() {
			log.Printf("[DEBUG] Cleaning up download for mod ID: %d", id)
			// Cleanup
			downloadsMutex.Lock()
			delete(activeDownloads, id)
			downloadsMutex.Unlock()
			close(progressChan)
		}()

		// Download the mod
		log.Printf("[DEBUG] Calling DownloadModWithProgress for workshop ID: %s", targetMod.WorkshopID)
		modPath, err := depot.DownloadModWithProgress(targetMod.WorkshopID, config.Username, config.Password, ModsDirectory, progressChan)
		if err != nil {
			log.Printf("Failed to download mod %s: %v\n", targetMod.Name, err)
			progressChan <- depot.ProgressUpdate{
				Stage:   "error",
				Message: fmt.Sprintf("Download failed: %v", err),
				Error:   err,
			}
			return
		}

		// Update database
		if err := database.UpdateModDownloaded(id, true, modPath); err != nil {
			log.Printf("Failed to update mod status: %v\n", err)
		}

		// Sync to addons directory with symlinks
		log.Printf("[DEBUG] Syncing mod to addons directory")
		if err := utils.SyncModToAddons(modPath); err != nil {
			log.Printf("[ERROR] Failed to sync mod to addons: %v", err)
			// Don't fail the download, just log the error
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started", "id": idStr})
}

// DownloadProgressHandler handles SSE for download progress
func DownloadProgressHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid mod ID", http.StatusBadRequest)
		return
	}
	log.Printf("[DEBUG] SSE connection requested for mod ID: %d", id)

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Wait for the progress channel to be registered (with timeout)
	var progressChan chan depot.ProgressUpdate
	var exists bool

	log.Printf("[DEBUG] Waiting for download to be registered in activeDownloads...")
	for i := 0; i < 50; i++ { // Wait up to 5 seconds
		downloadsMutex.RLock()
		progressChan, exists = activeDownloads[id]
		downloadsMutex.RUnlock()

		if exists {
			log.Printf("[DEBUG] Found active download for mod ID: %d after %d attempts", id, i+1)
			break
		}

		// Send a waiting message on first attempt
		if i == 0 {
			fmt.Fprintf(w, "data: {\"stage\":\"waiting\",\"message\":\"Waiting for download to start...\"}\n\n")
			flusher.Flush()
		}

		// Wait 100ms before checking again
		time.Sleep(100 * time.Millisecond)
	}

	if !exists {
		log.Printf("[ERROR] Download not found for mod ID: %d after timeout", id)
		fmt.Fprintf(w, "data: {\"stage\":\"error\",\"message\":\"Download not found or timed out\"}\n\n")
		flusher.Flush()
		return
	}

	log.Printf("[DEBUG] Starting to stream progress updates for mod ID: %d", id)
	// Stream progress updates
	for update := range progressChan {
		log.Printf("[PROGRESS] Mod %d: stage=%s, msg=%s, percent=%.2f%%, file=%s", id, update.Stage, update.Message, update.Percentage, update.FileName)
		data, err := json.Marshal(update)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Send final message and close gracefully
		if update.Stage == "complete" || update.Stage == "error" {
			log.Printf("[DEBUG] Sending final SSE message for mod %d, closing connection", id)
			// Give the client time to process the final message
			time.Sleep(100 * time.Millisecond)
			break
		}
	}

	log.Printf("[DEBUG] SSE stream completed for mod ID: %d", id)
}

func DeleteModHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid mod ID", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] Deleting mod ID: %d", id)

	// Get mod info before deleting from DB
	mod, err := database.GetMod(id)
	if err != nil {
		log.Printf("[ERROR] Failed to get mod info: %v", err)
		http.Error(w, "Failed to get mod info", http.StatusInternalServerError)
		return
	}

	// Remove from addons directory if mod path exists
	if mod.LocalPath != "" {
		log.Printf("[DEBUG] Removing mod from addons directory")
		if err := utils.RemoveModFromAddons(mod.LocalPath); err != nil {
			log.Printf("[ERROR] Failed to remove mod from addons: %v", err)
			// Continue anyway
		}
	}

	// Delete files from disk if they exist
	if mod.LocalPath != "" {
		log.Printf("[DEBUG] Deleting mod files at: %s", mod.LocalPath)
		if err := os.RemoveAll(mod.LocalPath); err != nil {
			log.Printf("[ERROR] Failed to delete mod files: %v", err)
			// Continue anyway to remove DB record
		} else {
			log.Printf("[DEBUG] Successfully deleted mod files")
		}
	}

	// Delete from database
	if err := database.DeleteMod(id); err != nil {
		log.Printf("[ERROR] Failed to delete mod from database: %v", err)
		http.Error(w, "Failed to delete mod", http.StatusInternalServerError)
		return
	}

	log.Printf("[DEBUG] Successfully deleted mod ID: %d", id)
	http.Redirect(w, r, "/mods", http.StatusSeeOther)
}

func StartupCommandHandler(w http.ResponseWriter, r *http.Request) {
	modsetIDStr := r.URL.Query().Get("modset")

	var mods []database.Mod
	var err error

	if modsetIDStr != "" {
		modsetID, _ := strconv.Atoi(modsetIDStr)
		mods, err = database.GetMods(modsetID)
	} else {
		http.Error(w, "Modset ID required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Failed to fetch mods", http.StatusInternalServerError)
		return
	}

	// Filter only downloaded mods
	var modPaths []string
	for _, mod := range mods {
		if mod.Downloaded && mod.LocalPath != "" {
			modPaths = append(modPaths, mod.LocalPath)
		}
	}

	// Extract keys
	if err := utils.ExtractKeys(modPaths, KeysDirectory); err != nil {
		log.Printf("Failed to extract keys: %v\n", err)
	}

	// Generate startup command
	var modParams []string
	for _, path := range modPaths {
		// Get the mod folder name (e.g., 123456)
		modName := filepath.Base(path)
		// Prepend "mods/" for the symlink path
		modParams = append(modParams, "mods/"+modName)
	}

	command := fmt.Sprintf("mods=\"%s\"", strings.Join(modParams, ";"))

	// Return as both HTML and JSON
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"command":  command,
			"keysPath": KeysDirectory,
		})
	} else {
		tmpl := template.Must(template.ParseFiles("templates/startup.html"))
		data := map[string]interface{}{
			"Command":  command,
			"KeysPath": KeysDirectory,
			"ModPaths": modPaths,
		}
		tmpl.Execute(w, data)
	}
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, nil)
		return
	}

	if r.Method == "POST" {
		password := r.FormValue("password")

		if auth.ValidatePassword(password) {
			if err := auth.CreateSession(w); err != nil {
				log.Printf("Failed to create session: %v", err)
				tmpl := template.Must(template.ParseFiles("templates/login.html"))
				tmpl.Execute(w, map[string]string{"Error": "Failed to create session"})
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		tmpl := template.Must(template.ParseFiles("templates/login.html"))
		tmpl.Execute(w, map[string]string{"Error": "Invalid password"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	auth.DestroySession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
