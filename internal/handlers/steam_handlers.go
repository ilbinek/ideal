package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"arma3modmanager/internal/steam"
)

// SteamStatusHandler returns the current Steam login state.
func SteamStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, steam.Default.Status())
}

// SteamLoginStreamHandler streams Steam login state changes over SSE so the UI
// can react to Steam Guard prompts and QR challenges as they happen.
func SteamLoginStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch := steam.Default.Subscribe()
	defer steam.Default.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(state)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// SteamLoginHandler starts an interactive login (credentials + 2FA, or QR).
func SteamLoginHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Method   string `json:"method"` // "credentials" | "qr"
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.Method == "" {
		body.Method = steam.MethodCredentials
	}
	if err := steam.Default.StartLogin(body.Method, body.Username, body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

// SteamGuardHandler submits a Steam Guard code to an in-progress login.
func SteamGuardHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := steam.Default.SubmitGuardCode(body.Code); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// SteamLogoutHandler closes the live Steam session.
func SteamLogoutHandler(w http.ResponseWriter, r *http.Request) {
	steam.Default.Logout()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
