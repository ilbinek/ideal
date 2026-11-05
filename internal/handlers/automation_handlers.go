package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/automation"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/server"
)

const (
	maxRestartSlots    = 24
	maxWarnMinutes     = 10
	maxWarnMinutesEach = 120
	maxBootSeconds     = 3600
	maxTemplateLength  = 150
)

// serverAutomationResponse is the automation tab's data. The RCON status never
// contains the password.
type serverAutomationResponse struct {
	*database.ServerAutomation
	Failures int               `json:"failures"`
	RCon     server.RConStatus `json:"rcon"`
}

type serverAutomationRequest struct {
	Autostart       bool                   `json:"autostart"`
	RestartOnCrash  bool                   `json:"restartOnCrash"`
	ScheduleEnabled bool                   `json:"scheduleEnabled"`
	WarnMinutes     []int                  `json:"warnMinutes"`
	RestartSlots    []database.RestartSlot `json:"restartSlots"`
}

// normalize checks the request and puts warnings in descending, unique order.
func (r *serverAutomationRequest) normalize() error {
	if len(r.RestartSlots) > maxRestartSlots {
		return fmt.Errorf("at most %d restart times are allowed", maxRestartSlots)
	}
	if r.RestartSlots == nil {
		r.RestartSlots = []database.RestartSlot{}
	}
	for _, s := range r.RestartSlots {
		if s.Minute < 0 || s.Minute > 1439 {
			return fmt.Errorf("restart time out of range")
		}
		if s.Days < 1 || s.Days > 127 {
			return fmt.Errorf("each restart time needs at least one day")
		}
	}
	if r.ScheduleEnabled && len(r.RestartSlots) == 0 {
		return fmt.Errorf("add a restart time before enabling scheduled restarts")
	}

	if len(r.WarnMinutes) > maxWarnMinutes {
		return fmt.Errorf("at most %d warnings are allowed", maxWarnMinutes)
	}
	seen := make(map[int]bool, len(r.WarnMinutes))
	warn := make([]int, 0, len(r.WarnMinutes))
	for _, m := range r.WarnMinutes {
		if m < 1 || m > maxWarnMinutesEach {
			return fmt.Errorf("warnings must be between 1 and %d minutes before the restart", maxWarnMinutesEach)
		}
		if !seen[m] {
			seen[m] = true
			warn = append(warn, m)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(warn)))
	r.WarnMinutes = warn
	return nil
}

func serverIDFromPath(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return 0, false
	}
	if _, err := database.GetServer(id); err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return 0, false
	}
	return id, true
}

func writeServerAutomation(w http.ResponseWriter, id int) {
	a, err := database.GetServerAutomation(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load automation settings")
		return
	}
	rcon, err := server.GetRConStatus(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read RCON settings")
		return
	}
	writeJSON(w, http.StatusOK, serverAutomationResponse{
		ServerAutomation: a,
		Failures:         len(a.CrashHistory),
		RCon:             rcon,
	})
}

// GetServerAutomationHandler returns a server's automation settings and state.
func GetServerAutomationHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDFromPath(w, r)
	if !ok {
		return
	}
	writeServerAutomation(w, id)
}

// PutServerAutomationHandler validates everything, then saves it in one write.
func PutServerAutomationHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDFromPath(w, r)
	if !ok {
		return
	}
	var body serverAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := body.normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := database.SaveServerAutomation(id, body.Autostart, body.RestartOnCrash, body.ScheduleEnabled,
		body.WarnMinutes, body.RestartSlots, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save automation settings")
		return
	}
	reloadAutomation()
	writeServerAutomation(w, id)
}

// GenerateRConHandler fills in missing RCON settings for a server.
func GenerateRConHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDFromPath(w, r)
	if !ok {
		return
	}
	result, err := server.GenerateRConConfig(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ListServerEventsHandler returns a page of a server's history, newest first.
func ListServerEventsHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDFromPath(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	events, err := database.ListServerEvents(id, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load events")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// GetAutomationSettingsHandler returns the global automation settings.
func GetAutomationSettingsHandler(w http.ResponseWriter, r *http.Request) {
	s, err := database.GetAutomationSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load automation settings")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// PutAutomationSettingsHandler saves the global automation settings.
func PutAutomationSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var body database.AutomationSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	body.WarningTemplate = strings.TrimSpace(body.WarningTemplate)
	switch {
	case body.BootDelaySec < 0 || body.BootDelaySec > maxBootSeconds,
		body.BootGapSec < 0 || body.BootGapSec > maxBootSeconds:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("boot timings must be between 0 and %d seconds", maxBootSeconds))
		return
	case body.WarningTemplate == "":
		writeError(w, http.StatusBadRequest, "warning message cannot be empty")
		return
	case len([]rune(body.WarningTemplate)) > maxTemplateLength:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("warning message is limited to %d characters", maxTemplateLength))
		return
	}
	if err := database.SaveAutomationSettings(body); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save automation settings")
		return
	}
	reloadAutomation()
	writeJSON(w, http.StatusOK, body)
}

// reloadAutomation tells the coordinator its settings changed.
func reloadAutomation() {
	automation.Default.Reload()
}
