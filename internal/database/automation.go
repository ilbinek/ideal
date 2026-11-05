package database

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Values for ServerAutomation.OwedStart: a start promised but not yet attempted,
// kept so it survives Ideal dying in between.
const (
	OwedNone    = ""
	OwedManual  = "manual"
	OwedPlanned = "planned"
	OwedCrash   = "crash"
)

// Event kinds recorded in a server's history.
const (
	EventStarted         = "started"
	EventStartFailed     = "start_failed"
	EventStopped         = "stopped"
	EventStopFailed      = "stop_failed"
	EventForceStop       = "force_stop"
	EventRestarted       = "restarted"
	EventExited          = "exited"
	EventRetryScheduled  = "retry_scheduled"
	EventGaveUp          = "gave_up"
	EventPlannedRestart  = "planned_restart"
	EventPlannedSkipped  = "planned_skipped"
	EventPlannedMissed   = "planned_missed"
	EventWarningSent     = "warning_sent"
	EventBootStart       = "boot_start"
	EventContentChanged  = "content_changed"
	EventIdentityUnknown = "identity_unknown"
	EventPortHeld        = "port_held"
)

// maxEventsPerServer caps each server's history.
const maxEventsPerServer = 500

// RestartSlot is one scheduled restart time, in UTC.
type RestartSlot struct {
	Minute int `json:"m"` // minute of the day, 0-1439
	Days   int `json:"d"` // weekday mask, bit 0 = Sunday
}

// ServerAutomation is a server's automation settings and runtime intent.
type ServerAutomation struct {
	ServerID        int           `json:"serverId"`
	Autostart       bool          `json:"autostart"`
	RestartOnCrash  bool          `json:"restartOnCrash"`
	ScheduleEnabled bool          `json:"scheduleEnabled"`
	WarnMinutes     []int         `json:"warnMinutes"`
	RestartSlots    []RestartSlot `json:"restartSlots"`

	// Runtime intent, changed only by what happens to the server.
	UserStopped  bool    `json:"userStopped"`
	OwedStart    string  `json:"-"`
	CrashHistory []int64 `json:"-"` // unix seconds of recent failures
	GaveUpAt     int64   `json:"gaveUpAt"`
	LastSlotUnix int64   `json:"-"`
}

// AutomationSettings are the global automation settings.
type AutomationSettings struct {
	Paused          bool   `json:"paused"`
	BootDelaySec    int    `json:"bootDelaySec"`
	BootGapSec      int    `json:"bootGapSec"`
	WarningTemplate string `json:"warningTemplate"`
}

// ServerEvent is one entry in a server's history.
type ServerEvent struct {
	ID       int64     `json:"id"`
	ServerID int       `json:"serverId"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Actor    string    `json:"actor"`
	Detail   string    `json:"detail"`
}

// DefaultWarningTemplate is the message shown to players before a planned restart.
const DefaultWarningTemplate = "Server restarting in {minutes} minute(s)"

func defaultWarnMinutes() []int { return []int{15, 5, 1} }

func migrateAutomation() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS server_automation (
			server_id INTEGER PRIMARY KEY,
			autostart BOOLEAN NOT NULL DEFAULT 0,
			restart_on_crash BOOLEAN NOT NULL DEFAULT 0,
			schedule_enabled BOOLEAN NOT NULL DEFAULT 0,
			warn_minutes TEXT NOT NULL DEFAULT '[15,5,1]',
			restart_slots TEXT NOT NULL DEFAULT '[]',
			user_stopped BOOLEAN NOT NULL DEFAULT 0,
			owed_start TEXT NOT NULL DEFAULT '',
			crash_history TEXT NOT NULL DEFAULT '[]',
			gave_up_at INTEGER NOT NULL DEFAULT 0,
			last_slot_unix INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS automation_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			paused BOOLEAN NOT NULL DEFAULT 0,
			boot_delay_sec INTEGER NOT NULL DEFAULT 30,
			boot_gap_sec INTEGER NOT NULL DEFAULT 30,
			warning_template TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS server_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			at INTEGER NOT NULL,
			kind TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_events_server ON server_events(server_id, id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO automation_settings (id, warning_template) VALUES (1, ?)`, DefaultWarningTemplate)
	return err
}

const automationCols = `server_id, autostart, restart_on_crash, schedule_enabled, warn_minutes,
	restart_slots, user_stopped, owed_start, crash_history, gave_up_at, last_slot_unix`

func scanAutomation(scan func(...any) error) (*ServerAutomation, error) {
	var a ServerAutomation
	var warn, slots, history string
	if err := scan(&a.ServerID, &a.Autostart, &a.RestartOnCrash, &a.ScheduleEnabled, &warn,
		&slots, &a.UserStopped, &a.OwedStart, &history, &a.GaveUpAt, &a.LastSlotUnix); err != nil {
		return nil, err
	}
	// A corrupt value shouldn't stop a server from being managed; fall back to empty.
	if json.Unmarshal([]byte(warn), &a.WarnMinutes) != nil || a.WarnMinutes == nil {
		a.WarnMinutes = []int{}
	}
	if json.Unmarshal([]byte(slots), &a.RestartSlots) != nil || a.RestartSlots == nil {
		a.RestartSlots = []RestartSlot{}
	}
	if json.Unmarshal([]byte(history), &a.CrashHistory) != nil || a.CrashHistory == nil {
		a.CrashHistory = []int64{}
	}
	return &a, nil
}

// GetServerAutomation returns a server's automation row, or defaults if it has none.
func GetServerAutomation(serverID int) (*ServerAutomation, error) {
	row := db.QueryRow(`SELECT `+automationCols+` FROM server_automation WHERE server_id = ?`, serverID)
	a, err := scanAutomation(row.Scan)
	if err == sql.ErrNoRows {
		return &ServerAutomation{
			ServerID:     serverID,
			WarnMinutes:  defaultWarnMinutes(),
			RestartSlots: []RestartSlot{},
			CrashHistory: []int64{},
		}, nil
	}
	return a, err
}

// GetAllServerAutomation returns the rows that exist, keyed by server. Servers with
// no row have default settings, which means everything is off.
func GetAllServerAutomation() (map[int]*ServerAutomation, error) {
	rows, err := db.Query(`SELECT ` + automationCols + ` FROM server_automation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]*ServerAutomation)
	for rows.Next() {
		a, err := scanAutomation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[a.ServerID] = a
	}
	return out, rows.Err()
}

// SaveServerAutomation stores a server's settings without touching runtime intent.
// lastSlotUnix marks every slot up to then as already handled, so saving a schedule
// never fires a slot that has just gone by.
func SaveServerAutomation(serverID int, autostart, restartOnCrash, scheduleEnabled bool, warnMinutes []int, slots []RestartSlot, lastSlotUnix int64) error {
	warn, err := json.Marshal(warnMinutes)
	if err != nil {
		return err
	}
	slotJSON, err := json.Marshal(slots)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO server_automation
			(server_id, autostart, restart_on_crash, schedule_enabled, warn_minutes, restart_slots, last_slot_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET
			autostart = excluded.autostart,
			restart_on_crash = excluded.restart_on_crash,
			schedule_enabled = excluded.schedule_enabled,
			warn_minutes = excluded.warn_minutes,
			restart_slots = excluded.restart_slots,
			last_slot_unix = excluded.last_slot_unix`,
		serverID, autostart, restartOnCrash, scheduleEnabled, string(warn), string(slotJSON), lastSlotUnix)
	return err
}

// updateAutomationRow applies a change, creating the row first if needed.
func updateAutomationRow(serverID int, set string, args ...any) error {
	if _, err := db.Exec(`INSERT OR IGNORE INTO server_automation (server_id) VALUES (?)`, serverID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE server_automation SET `+set+` WHERE server_id = ?`, append(args, serverID)...)
	return err
}

// RecordManualStart: a person wants this server up, so any earlier stop, pending
// retry or give-up no longer applies.
func RecordManualStart(serverID int) error {
	return updateAutomationRow(serverID, `user_stopped = 0, owed_start = '', crash_history = '[]', gave_up_at = 0`)
}

// RecordManualStop: a person wants this server down until someone starts it.
func RecordManualStop(serverID int) error {
	return updateAutomationRow(serverID, `user_stopped = 1, owed_start = ''`)
}

// RecordManualRestart: a person wants this server up, and it's owed a start until
// the restart's start half has been attempted.
func RecordManualRestart(serverID int) error {
	return updateAutomationRow(serverID, `user_stopped = 0, owed_start = ?, crash_history = '[]', gave_up_at = 0`, OwedManual)
}

// SetOwedStart records or clears a pending start.
func SetOwedStart(serverID int, owed string) error {
	return updateAutomationRow(serverID, `owed_start = ?`, owed)
}

// SetCrashState records the failure history and what's to happen next.
func SetCrashState(serverID int, history []int64, owed string, gaveUpAt int64) error {
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	return updateAutomationRow(serverID, `crash_history = ?, owed_start = ?, gave_up_at = ?`, string(data), owed, gaveUpAt)
}

// SetLastSlot records the newest schedule slot that has been handled.
func SetLastSlot(serverID int, slotUnix int64) error {
	return updateAutomationRow(serverID, `last_slot_unix = ?`, slotUnix)
}

// GetAutomationSettings returns the global automation settings.
func GetAutomationSettings() (*AutomationSettings, error) {
	var s AutomationSettings
	err := db.QueryRow(`SELECT paused, boot_delay_sec, boot_gap_sec, warning_template FROM automation_settings WHERE id = 1`).
		Scan(&s.Paused, &s.BootDelaySec, &s.BootGapSec, &s.WarningTemplate)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveAutomationSettings stores the global automation settings.
func SaveAutomationSettings(s AutomationSettings) error {
	_, err := db.Exec(`UPDATE automation_settings SET paused = ?, boot_delay_sec = ?, boot_gap_sec = ?, warning_template = ? WHERE id = 1`,
		s.Paused, s.BootDelaySec, s.BootGapSec, s.WarningTemplate)
	return err
}

// AddServerEvent appends to a server's history, dropping the oldest past the cap.
func AddServerEvent(serverID int, kind, actor, detail string) error {
	if _, err := db.Exec(`INSERT INTO server_events (server_id, at, kind, actor, detail) VALUES (?, ?, ?, ?, ?)`,
		serverID, time.Now().Unix(), kind, actor, detail); err != nil {
		return err
	}
	_, err := db.Exec(`
		DELETE FROM server_events WHERE server_id = ? AND id NOT IN (
			SELECT id FROM server_events WHERE server_id = ? ORDER BY id DESC LIMIT ?
		)`, serverID, serverID, maxEventsPerServer)
	return err
}

// ListServerEvents returns a server's history, newest first. Pass the last ID seen
// as before to get the next page, or 0 for the newest.
func ListServerEvents(serverID, limit int, before int64) ([]ServerEvent, error) {
	if limit <= 0 || limit > maxEventsPerServer {
		limit = 100
	}
	query := `SELECT id, server_id, at, kind, actor, detail FROM server_events WHERE server_id = ?`
	args := []any{serverID}
	if before > 0 {
		query += ` AND id < ?`
		args = append(args, before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []ServerEvent{}
	for rows.Next() {
		var e ServerEvent
		var at int64
		if err := rows.Scan(&e.ID, &e.ServerID, &at, &e.Kind, &e.Actor, &e.Detail); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0).UTC()
		events = append(events, e)
	}
	return events, rows.Err()
}
