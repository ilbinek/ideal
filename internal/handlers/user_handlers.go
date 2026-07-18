package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"arma3modmanager/internal/auth"
	"arma3modmanager/internal/database"
)

var steamID64Re = regexp.MustCompile(`^7656119\d{10}$`)

func ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := database.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SteamID           string `json:"steamId"`
		IsFullAdmin       bool   `json:"isFullAdmin"`
		CanManageMods     bool   `json:"canManageMods"`
		CanManageBranches bool   `json:"canManageBranches"`
		ServerIDs         []int  `json:"serverIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	steamID := strings.TrimSpace(body.SteamID)
	if !steamID64Re.MatchString(steamID) {
		writeError(w, http.StatusBadRequest, "steamId must be a 17-digit SteamID64 (7656119…)")
		return
	}
	if existing, err := database.GetUserBySteamID(steamID); err == nil && existing != nil {
		writeError(w, http.StatusConflict, "user with this SteamID already exists")
		return
	} else if err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to check existing user")
		return
	}

	displayName, avatarURL := auth.FetchSteamPersona(steamID)
	user, err := database.CreateSteamUser(steamID, displayName, avatarURL)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "user with this SteamID already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}

	if body.ServerIDs == nil {
		body.ServerIDs = []int{}
	}
	if body.IsFullAdmin {
		body.CanManageMods = true
		body.CanManageBranches = true
	}
	if body.IsFullAdmin || body.CanManageMods || body.CanManageBranches || len(body.ServerIDs) > 0 {
		if err := database.UpdateUserPermissions(user.ID, body.IsFullAdmin, body.CanManageMods, body.CanManageBranches, body.ServerIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set permissions: "+err.Error())
			return
		}
		user, err = database.GetUser(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
	}

	writeJSON(w, http.StatusOK, user)
}

func UpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var body struct {
		IsFullAdmin       bool  `json:"isFullAdmin"`
		CanManageMods     bool  `json:"canManageMods"`
		CanManageBranches bool  `json:"canManageBranches"`
		ServerIDs         []int `json:"serverIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if body.ServerIDs == nil {
		body.ServerIDs = []int{}
	}
	if body.IsFullAdmin {
		body.CanManageMods = true
		body.CanManageBranches = true
	}

	if err := database.UpdateUserPermissions(id, body.IsFullAdmin, body.CanManageMods, body.CanManageBranches, body.ServerIDs); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update user: "+err.Error())
		return
	}

	user, err := database.GetUser(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	_ = database.DeleteSessionsForUser(id)
	if err := database.DeleteUser(id); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
