package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/server"
)

func GetHeadlessHandler(w http.ResponseWriter, r *http.Request) {
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
	clients, err := server.Default.ListHeadless(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if clients == nil {
		clients = []server.HeadlessStatusSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":   srv.HeadlessCount,
		"clients": clients,
	})
}

func PutHeadlessHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}
	if _, err := database.GetServer(id); err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := server.Default.SetHeadlessCount(id, body.Count); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	clients, err := server.Default.ListHeadless(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if clients == nil {
		clients = []server.HeadlessStatusSnapshot{}
	}
	srv, _ := database.GetServer(id)
	count := 0
	if srv != nil {
		count = srv.HeadlessCount
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":   count,
		"clients": clients,
	})
}

func StartHeadlessHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	if err := server.Default.StartHeadless(id, slot); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func StopHeadlessHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	if err := server.Default.StopHeadless(id, slot); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func RestartHeadlessHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	if err := server.Default.RestartHeadless(id, slot); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func ListHeadlessLogsHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	hc, err := database.GetHeadlessClient(id, slot)
	if err != nil {
		writeError(w, http.StatusNotFound, "headless client not found")
		return
	}
	logs, err := server.ListHeadlessLogs(srv.Slug, slot, hc.ProfileName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []server.LogEntry{}
	}
	if sessionPath := server.Default.ActiveHeadlessSessionLogPath(id, slot); sessionPath != "" {
		if info, err := os.Stat(sessionPath); err == nil {
			name := filepath.Base(sessionPath)
			found := false
			for _, entry := range logs {
				if entry.Name == name {
					found = true
					break
				}
			}
			if !found {
				logs = append([]server.LogEntry{{
					Name:    name,
					Path:    sessionPath,
					Size:    info.Size(),
					ModTime: info.ModTime(),
				}}, logs...)
			}
		}
	}
	writeJSON(w, http.StatusOK, logs)
}

func GetHeadlessLogHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	filename := r.PathValue("filename")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if _, err := database.GetHeadlessClient(id, slot); err != nil {
		writeError(w, http.StatusNotFound, "headless client not found")
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

func DownloadHeadlessLogHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	filename := r.PathValue("filename")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	hc, err := database.GetHeadlessClient(id, slot)
	if err != nil {
		writeError(w, http.StatusNotFound, "headless client not found")
		return
	}
	f, name, err := server.OpenHeadlessLogForDownload(srv.Slug, slot, hc.ProfileName, filename)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func DeleteHeadlessLogHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}
	filename := r.PathValue("filename")
	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	hc, err := database.GetHeadlessClient(id, slot)
	if err != nil {
		writeError(w, http.StatusNotFound, "headless client not found")
		return
	}
	if err := server.DeleteHeadlessLog(id, slot, srv.Slug, hc.ProfileName, filename); err != nil {
		msg := err.Error()
		status := http.StatusInternalServerError
		if strings.Contains(msg, "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(msg, "cannot delete") || strings.Contains(msg, "invalid") {
			status = http.StatusConflict
		}
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func HeadlessLogStreamHandler(w http.ResponseWriter, r *http.Request) {
	id, slot, ok := parseServerSlot(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, okFlush := w.(http.Flusher)
	if !okFlush {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	srv, err := database.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	hc, err := database.GetHeadlessClient(id, slot)
	if err != nil {
		writeError(w, http.StatusNotFound, "headless client not found")
		return
	}

	const liveSeedBytes = 256 * 1024
	var rptOffset int64
	var sessionOffset int64
	rptLocked := false
	sessionSeeded := false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	send := func(chunk, source string) {
		if chunk == "" {
			return
		}
		data, _ := json.Marshal(map[string]string{"chunk": chunk, "path": source})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			rptPath := server.ResolveHeadlessRPTPath(
				srv.Slug,
				hc.ProfileName,
				server.Default.ActiveHeadlessRPTPath(id, slot),
			)
			if rptPath != "" {
				if !rptLocked {
					chunk, size, err := server.ReadLogTail(rptPath, liveSeedBytes)
					if err == nil {
						rptOffset = size
						rptLocked = true
						send(chunk, rptPath)
					}
					continue
				}
				c, newOffset, err := server.TailLog(rptPath, rptOffset)
				if err == nil {
					rptOffset = newOffset
					if c != "" {
						send(c, rptPath)
					}
				}
				continue
			}

			sessionPath := server.Default.ActiveHeadlessSessionLogPath(id, slot)
			if sessionPath == "" {
				continue
			}
			if !sessionSeeded {
				chunk, size, err := server.ReadLogTail(sessionPath, liveSeedBytes)
				if err == nil {
					sessionOffset = size
					sessionSeeded = true
					send(chunk, sessionPath)
				}
				continue
			}
			c, newOffset, err := server.TailLog(sessionPath, sessionOffset)
			if err == nil {
				sessionOffset = newOffset
				if c != "" {
					send(c, sessionPath)
				}
			}
		}
	}
}

func parseServerSlot(w http.ResponseWriter, r *http.Request) (id, slot int, ok bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return 0, 0, false
	}
	slot, err = strconv.Atoi(r.PathValue("slot"))
	if err != nil || slot < 1 {
		writeError(w, http.StatusBadRequest, "invalid headless slot")
		return 0, 0, false
	}
	return id, slot, true
}
