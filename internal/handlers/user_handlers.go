package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"arma3modmanager/internal/database"
)

func ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := database.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
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
