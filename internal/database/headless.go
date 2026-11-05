package database

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// HeadlessClient is one configured HC slot for a server.
type HeadlessClient struct {
	ID          int    `json:"id"`
	ServerID    int    `json:"serverId"`
	Slot        int    `json:"slot"`
	ProfileName string `json:"profileName"`
	Status      string `json:"status"`
	PID         int    `json:"pid"`
	PIDCreateMs int64  `json:"-"` // start time of PID, proves it wasn't reused
	ActiveRPT   string `json:"activeRpt"`
}

func migrateHeadless() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS server_headless_clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			slot INTEGER NOT NULL,
			profile_name TEXT NOT NULL,
			status TEXT DEFAULT 'stopped',
			pid INTEGER DEFAULT 0,
			active_rpt TEXT DEFAULT '',
			UNIQUE(server_id, slot),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_headless_server ON server_headless_clients(server_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	const addIdentity = "ALTER TABLE server_headless_clients ADD COLUMN pid_create_ms INTEGER NOT NULL DEFAULT 0"
	if _, err := db.Exec(addIdentity); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}

// SanitizeHCProfileBase turns a server display name into a safe Arma profile base.
func SanitizeHCProfileBase(name, slugFallback string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		out = strings.TrimSpace(slugFallback)
	}
	if out == "" {
		out = "Server"
	}
	return out
}

// HeadlessProfileName returns the -name / profile for slot N (1-based).
func HeadlessProfileName(serverName, slug string, slot int) string {
	return fmt.Sprintf("%s-HC%d", SanitizeHCProfileBase(serverName, slug), slot)
}

func GetServerHeadlessCount(serverID int) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COALESCE(headless_count, 0) FROM servers WHERE id = ?`, serverID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func SetServerHeadlessCount(serverID, count int) error {
	if count < 0 {
		count = 0
	}
	_, err := db.Exec(`
		UPDATE servers SET headless_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		count, serverID,
	)
	return err
}

func GetHeadlessClients(serverID int) ([]HeadlessClient, error) {
	rows, err := db.Query(`
		SELECT id, server_id, slot, profile_name, status, pid, COALESCE(pid_create_ms, 0), COALESCE(active_rpt, '')
		FROM server_headless_clients
		WHERE server_id = ?
		ORDER BY slot`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []HeadlessClient
	for rows.Next() {
		var c HeadlessClient
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Slot, &c.ProfileName, &c.Status, &c.PID, &c.PIDCreateMs, &c.ActiveRPT); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func GetHeadlessClient(serverID, slot int) (*HeadlessClient, error) {
	var c HeadlessClient
	err := db.QueryRow(`
		SELECT id, server_id, slot, profile_name, status, pid, COALESCE(pid_create_ms, 0), COALESCE(active_rpt, '')
		FROM server_headless_clients
		WHERE server_id = ? AND slot = ?`, serverID, slot).
		Scan(&c.ID, &c.ServerID, &c.Slot, &c.ProfileName, &c.Status, &c.PID, &c.PIDCreateMs, &c.ActiveRPT)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func UpdateHeadlessStatus(serverID, slot int, status string, pid int, activeRPT string) error {
	_, err := db.Exec(`
		UPDATE server_headless_clients
		SET status = ?, pid = ?, active_rpt = ?,
			pid_create_ms = CASE WHEN ? = 0 THEN 0 ELSE pid_create_ms END
		WHERE server_id = ? AND slot = ?`,
		status, pid, activeRPT, pid, serverID, slot,
	)
	return err
}

// UpdateHeadlessProcess records a launched or adopted HC with its start time.
func UpdateHeadlessProcess(serverID, slot int, status string, pid int, createMs int64, activeRPT string) error {
	_, err := db.Exec(`
		UPDATE server_headless_clients
		SET status = ?, pid = ?, pid_create_ms = ?, active_rpt = ?
		WHERE server_id = ? AND slot = ?`,
		status, pid, createMs, activeRPT, serverID, slot,
	)
	return err
}

// SyncHeadlessSlots ensures slots 1..count exist with current profile names; deletes higher slots.
// Returns the resulting client list. Does not stop running processes — caller must.
func SyncHeadlessSlots(serverID int, count int, serverName, slug string) ([]HeadlessClient, error) {
	if count < 0 {
		count = 0
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE servers SET headless_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		count, serverID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`DELETE FROM server_headless_clients WHERE server_id = ? AND slot > ?`, serverID, count); err != nil {
		return nil, err
	}

	for slot := 1; slot <= count; slot++ {
		profile := HeadlessProfileName(serverName, slug, slot)
		var existingID int
		err := tx.QueryRow(`
			SELECT id FROM server_headless_clients WHERE server_id = ? AND slot = ?`,
			serverID, slot,
		).Scan(&existingID)
		switch {
		case err == sql.ErrNoRows:
			if _, err := tx.Exec(`
				INSERT INTO server_headless_clients (server_id, slot, profile_name, status, pid, active_rpt)
				VALUES (?, ?, ?, ?, 0, '')`,
				serverID, slot, profile, ServerStatusStopped,
			); err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		default:
			if _, err := tx.Exec(`
				UPDATE server_headless_clients SET profile_name = ? WHERE id = ?`,
				profile, existingID,
			); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetHeadlessClients(serverID)
}

// RefreshHeadlessProfileNames updates profile_name for all slots from the current server name.
func RefreshHeadlessProfileNames(serverID int, serverName, slug string) error {
	clients, err := GetHeadlessClients(serverID)
	if err != nil {
		return err
	}
	for _, c := range clients {
		profile := HeadlessProfileName(serverName, slug, c.Slot)
		if profile == c.ProfileName {
			continue
		}
		if _, err := db.Exec(`
			UPDATE server_headless_clients SET profile_name = ? WHERE id = ?`,
			profile, c.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func GetAllHeadlessClients() ([]HeadlessClient, error) {
	rows, err := db.Query(`
		SELECT id, server_id, slot, profile_name, status, pid, COALESCE(pid_create_ms, 0), COALESCE(active_rpt, '')
		FROM server_headless_clients
		ORDER BY server_id, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []HeadlessClient
	for rows.Next() {
		var c HeadlessClient
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Slot, &c.ProfileName, &c.Status, &c.PID, &c.PIDCreateMs, &c.ActiveRPT); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}
