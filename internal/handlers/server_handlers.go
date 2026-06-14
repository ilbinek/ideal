package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/server"
)

// --- Branches ---

func GetBranchesHandler(w http.ResponseWriter, r *http.Request) {
	branches, err := database.GetBranches()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch branches")
		return
	}
	if branches == nil {
		branches = []database.ArmaBranch{}
	}
	for i := range branches {
		installed := server.IsBranchInstalled(branches[i].InstallPath)
		branches[i].Installed = installed
		if installed {
			_ = database.SetBranchInstalled(branches[i].ID, true)
		}
	}
	writeJSON(w, http.StatusOK, branches)
}

func CreateBranchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		SteamBranch string `json:"steamBranch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	slug := server.SlugFromName(body.Name)
	installPath := server.BranchInstallDir(slug)
	id, err := database.CreateBranch(body.Name, body.SteamBranch, installPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create branch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func InstallBranchHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid branch id")
		return
	}
	if err := server.BranchInstallerDefault.Install(id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func BranchInstallStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch := server.BranchInstallerDefault.Subscribe()
	defer server.BranchInstallerDefault.Unsubscribe(ch)

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

// --- Servers ---

func GetServersHandler(w http.ResponseWriter, r *http.Request) {
	servers, err := database.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch servers")
		return
	}
	if servers == nil {
		servers = []database.Server{}
	}
	writeJSON(w, http.StatusOK, servers)
}

func CreateServerHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		BranchID    int    `json:"branchId"`
		ModsetID    *int   `json:"modsetId"`
		GamePort    int    `json:"gamePort"`
		SteamPort   int    `json:"steamPort"`
		ProfileName string `json:"profileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.Name == "" || body.BranchID == 0 {
		writeError(w, http.StatusBadRequest, "name and branchId required")
		return
	}
	if body.GamePort == 0 {
		body.GamePort = 2302
	}
	if body.SteamPort == 0 {
		body.SteamPort = body.GamePort + 1
	}

	baseSlug := server.SlugFromName(body.Name)
	slug := server.EnsureUniqueSlug(baseSlug, func(s string) bool {
		servers, _ := database.GetServers()
		for _, srv := range servers {
			if srv.Slug == s {
				return true
			}
		}
		return false
	})

	id, err := database.CreateServer(body.Name, slug, body.BranchID, body.ModsetID, body.GamePort, body.SteamPort, body.ProfileName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create server: "+err.Error())
		return
	}

	if body.ModsetID != nil {
		if err := database.SeedServerModsFromModset(int(id), *body.ModsetID); err != nil {
			log.Printf("[SERVER] failed to seed mods from modset: %v", err)
		}
	}

	if err := server.ProvisionServer(int(id)); err != nil {
		log.Printf("[SERVER] provision warning on create: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "slug": slug})
}

func GetServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	writeJSON(w, http.StatusOK, srv)
}

func UpdateServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	var body struct {
		Name              string `json:"name"`
		BranchID          int    `json:"branchId"`
		GamePort          int    `json:"gamePort"`
		SteamPort         int    `json:"steamPort"`
		ProfileName       string `json:"profileName"`
		ExtraLaunchParams string `json:"extraLaunchParams"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := database.UpdateServer(id, body.Name, body.BranchID, body.GamePort, body.SteamPort, body.ProfileName, body.ExtraLaunchParams); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update server")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func DeleteServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if srv.Status == database.ServerStatusRunning || srv.Status == database.ServerStatusStarting {
		writeError(w, http.StatusConflict, "stop the server before deleting")
		return
	}
	instanceDir := server.InstanceDir(srv.Slug)
	if err := database.DeleteServer(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete server")
		return
	}
	if err := os.RemoveAll(instanceDir); err != nil {
		log.Printf("[SERVER] failed to remove instance dir: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func ProvisionServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if err := server.ProvisionServer(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func StartServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if err := server.Default.Start(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func StopServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if err := server.Default.Stop(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func RestartServerHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if err := server.Default.Restart(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func ServerStatusHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	snap := server.Default.Snapshot()
	for _, s := range snap.Servers {
		if s.ID == id {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	writeError(w, http.StatusNotFound, "server not found")
}

func ServersStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch := server.Default.Subscribe()
	defer server.Default.Unsubscribe(ch)

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

func ServerLaunchPreviewHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	args, err := server.BuildLaunchArgs(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, args)
}

// --- Server mods ---

func GetServerModsHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	mods, err := database.GetServerMods(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch server mods")
		return
	}
	if mods == nil {
		mods = []database.ServerMod{}
	}
	writeJSON(w, http.StatusOK, mods)
}

func PutServerModsHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	var body []struct {
		ModID       *int   `json:"modId"`
		CustomPath  string `json:"customPath"`
		Label       string `json:"label"`
		IsServerMod bool   `json:"isServerMod"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var mods []database.ServerMod
	for _, item := range body {
		if item.ModID == nil && item.CustomPath == "" {
			continue
		}
		if item.CustomPath != "" {
			if err := server.ValidateCustomModPath(item.CustomPath); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		mods = append(mods, database.ServerMod{
			ServerID:    id,
			ModID:       item.ModID,
			CustomPath:  item.CustomPath,
			Label:       item.Label,
			IsServerMod: item.IsServerMod,
		})
	}

	if err := database.ReplaceServerMods(id, mods); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update server mods")
		return
	}
	if err := server.ProvisionServer(id); err != nil {
		log.Printf("[SERVER] provision after mod update: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Server keys ---

func GetServerKeysHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	customKeys, err := database.GetServerKeys(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch keys")
		return
	}
	autoKeys, _ := server.AutoKeysFromMods(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"custom": customKeys,
		"auto":   autoKeys,
	})
}

func UploadServerKeyHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}
	file, header, err := r.FormFile("key")
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read key file")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".bikey") {
		writeError(w, http.StatusBadRequest, "file must be a .bikey")
		return
	}

	keysStore := filepath.Join(server.InstanceDir(srv.Slug), "custom_keys")
	if err := os.MkdirAll(keysStore, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create keys store")
		return
	}
	destPath := filepath.Join(keysStore, header.Filename)
	out, err := os.Create(destPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save key")
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeError(w, http.StatusInternalServerError, "failed to write key")
		return
	}
	out.Close()

	keyID, err := database.AddServerKey(id, header.Filename, destPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record key")
		return
	}
	_ = server.ProvisionServer(id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": keyID})
}

func DeleteServerKeyHandler(w http.ResponseWriter, r *http.Request) {
	serverID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	keyID, err := strconv.Atoi(r.PathValue("keyId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	key, err := database.GetServerKey(keyID)
	if err != nil || key.ServerID != serverID {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	_ = os.Remove(key.SourcePath)
	if err := database.DeleteServerKey(keyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}
	_ = server.ProvisionServer(serverID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Config ---

func GetServerConfigHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	name := r.PathValue("name")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	content, err := server.ReadConfigFile(srv.Slug, name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func PutServerConfigHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	name := r.PathValue("name")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := server.WriteConfigFile(srv.Slug, name, body.Content); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Logs ---

func ListServerLogsHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	logs, err := server.ListLogs(srv.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []server.LogEntry{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func GetServerLogHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	filename := r.PathValue("filename")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	content, totalSize, err := server.ReadLog(srv.Slug, filename, offset, limit)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"content":   content,
		"totalSize": totalSize,
		"offset":    offset,
	})
}

func ServerLogStreamHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	var offset int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			rptPath := server.Default.ActiveRPTPath(id)
			if rptPath == "" {
				srv, err := database.GetServer(id)
				if err == nil {
					rptPath, _ = server.FindNewestRPT(srv.Slug)
				}
			}
			if rptPath == "" {
				continue
			}
			chunk, newOffset, err := server.TailLog(rptPath, offset)
			if err != nil {
				continue
			}
			offset = newOffset
			if chunk == "" {
				continue
			}
			data, _ := json.Marshal(map[string]string{"chunk": chunk, "path": rptPath})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- Missions ---

func ListMissionsHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	missions, err := server.ListMissions(srv.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if missions == nil {
		missions = []server.LogEntry{}
	}
	writeJSON(w, http.StatusOK, missions)
}

func UploadMissionHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	if err := r.ParseMultipartForm(server.MissionUploadMaxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}
	file, header, err := r.FormFile("mission")
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read mission file")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pbo") {
		writeError(w, http.StatusBadRequest, "file must be a .pbo")
		return
	}
	if strings.Contains(header.Filename, "..") || strings.Contains(header.Filename, "/") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	missionsDir := filepath.Join(server.InstanceDir(srv.Slug), "mpmissions")
	if err := os.MkdirAll(missionsDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create missions dir")
		return
	}
	destPath := filepath.Join(missionsDir, header.Filename)
	out, err := os.Create(destPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save mission")
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeError(w, http.StatusInternalServerError, "failed to write mission")
		return
	}
	out.Close()
	writeJSON(w, http.StatusOK, map[string]string{"name": header.Filename})
}

func DeleteMissionHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	name := r.PathValue("name")
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	path := filepath.Join(server.InstanceDir(srv.Slug), "mpmissions", name)
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusNotFound, "mission not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
