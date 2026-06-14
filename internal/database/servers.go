package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"arma3modmanager/internal/utils"
)

const (
	ServerStatusStopped  = "stopped"
	ServerStatusStarting = "starting"
	ServerStatusRunning  = "running"
	ServerStatusStopping = "stopping"
	ServerStatusUnknown  = "unknown"
)

type ArmaBranch struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	SteamBranch string    `json:"steamBranch"`
	InstallPath string    `json:"installPath"`
	Installed   bool      `json:"installed"`
	LastUpdated time.Time `json:"lastUpdated"`
}

type Server struct {
	ID                int       `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	BranchID          int       `json:"branchId"`
	ModsetID          *int      `json:"modsetId,omitempty"`
	GamePort          int       `json:"gamePort"`
	SteamPort         int       `json:"steamPort"`
	ProfileName       string    `json:"profileName"`
	ExtraLaunchParams string    `json:"extraLaunchParams"`
	Status            string    `json:"status"`
	PID               int       `json:"pid"`
	ActiveRPT         string    `json:"activeRpt"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ServerMod struct {
	ID          int    `json:"id"`
	ServerID    int    `json:"serverId"`
	ModID       *int   `json:"modId,omitempty"`
	CustomPath  string `json:"customPath,omitempty"`
	Label       string `json:"label"`
	IsServerMod bool   `json:"isServerMod"`
	SortOrder   int    `json:"sortOrder"`
}

type ServerKey struct {
	ID         int    `json:"id"`
	ServerID   int    `json:"serverId"`
	Filename   string `json:"filename"`
	SourcePath string `json:"sourcePath"`
}

func migrateServers() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS arma_branches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			steam_branch TEXT NOT NULL DEFAULT '',
			install_path TEXT NOT NULL,
			installed BOOLEAN DEFAULT 0,
			last_updated TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT NOT NULL UNIQUE,
			branch_id INTEGER NOT NULL,
			modset_id INTEGER,
			game_port INTEGER NOT NULL DEFAULT 2302,
			steam_port INTEGER NOT NULL DEFAULT 2303,
			profile_name TEXT NOT NULL DEFAULT 'Server',
			extra_launch_params TEXT DEFAULT '',
			status TEXT DEFAULT 'stopped',
			pid INTEGER DEFAULT 0,
			active_rpt TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (branch_id) REFERENCES arma_branches(id),
			FOREIGN KEY (modset_id) REFERENCES modsets(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_mods (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			mod_id INTEGER,
			custom_path TEXT,
			label TEXT NOT NULL,
			is_server_mod BOOLEAN DEFAULT 0,
			sort_order INTEGER DEFAULT 0,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE,
			FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS server_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			filename TEXT NOT NULL,
			source_path TEXT NOT NULL,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_mods_server ON server_mods(server_id)`,
		`CREATE INDEX IF NOT EXISTS idx_server_keys_server ON server_keys(server_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := seedDefaultBranches(); err != nil {
		return err
	}
	if err := fixPerformanceBranchCode(); err != nil {
		return err
	}
	return migrateBranchInstallPaths()
}

func fixPerformanceBranchCode() error {
	_, err := db.Exec(
		`UPDATE arma_branches SET steam_branch = 'profiling' WHERE name = 'Performance' AND steam_branch = 'performance'`,
	)
	return err
}

func migrateBranchInstallPaths() error {
	rows, err := db.Query("SELECT id, name, install_path FROM arma_branches")
	if err != nil {
		return err
	}

	type branchRow struct {
		id                int
		name, installPath string
	}
	var branches []branchRow
	for rows.Next() {
		var b branchRow
		if err := rows.Scan(&b.id, &b.name, &b.installPath); err != nil {
			rows.Close()
			return err
		}
		branches = append(branches, b)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, b := range branches {
		slug := branchSlug(b.name, b.installPath)
		newPath := utils.BranchInstallPath(slug)
		if filepath.Clean(utils.ResolveDataPath(b.installPath)) == filepath.Clean(newPath) {
			continue
		}
		if _, err := db.Exec("UPDATE arma_branches SET install_path = ? WHERE id = ?", newPath, b.id); err != nil {
			return err
		}
	}
	return nil
}

func branchSlug(name, installPath string) string {
	switch name {
	case "Main":
		return "main"
	case "Performance":
		return "performance"
	case "Creative Dev":
		return "creativedev"
	default:
		return filepath.Base(filepath.Clean(installPath))
	}
}

func seedDefaultBranches() error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM arma_branches").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	defaults := []struct {
		name, steamBranch, slug string
	}{
		{"Main", "", "main"},
		{"Performance", "profiling", "performance"},
		{"Creative Dev", "creativedev", "creativedev"},
	}
	for _, d := range defaults {
		path := utils.BranchInstallPath(d.slug)
		_, err := db.Exec(
			"INSERT INTO arma_branches (name, steam_branch, install_path) VALUES (?, ?, ?)",
			d.name, d.steamBranch, path,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func GetBranches() ([]ArmaBranch, error) {
	rows, err := db.Query(`
		SELECT id, name, steam_branch, install_path, installed, COALESCE(last_updated, '')
		FROM arma_branches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []ArmaBranch
	for rows.Next() {
		var b ArmaBranch
		var lastUpdated string
		if err := rows.Scan(&b.ID, &b.Name, &b.SteamBranch, &b.InstallPath, &b.Installed, &lastUpdated); err != nil {
			return nil, err
		}
		if lastUpdated != "" {
			b.LastUpdated, _ = time.Parse("2006-01-02 15:04:05", lastUpdated)
		}
		b.InstallPath = utils.ResolveDataPath(b.InstallPath)
		branches = append(branches, b)
	}
	return branches, rows.Err()
}

func GetBranch(id int) (*ArmaBranch, error) {
	var b ArmaBranch
	var lastUpdated sql.NullTime
	err := db.QueryRow(`
		SELECT id, name, steam_branch, install_path, installed, last_updated
		FROM arma_branches WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.SteamBranch, &b.InstallPath, &b.Installed, &lastUpdated)
	if err != nil {
		return nil, err
	}
	if lastUpdated.Valid {
		b.LastUpdated = lastUpdated.Time
	}
	b.InstallPath = utils.ResolveDataPath(b.InstallPath)
	return &b, nil
}

func DeleteBranch(id int) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM servers WHERE branch_id = ?", id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("branch is used by %d server(s)", count)
	}
	_, err := db.Exec("DELETE FROM arma_branches WHERE id = ?", id)
	return err
}

func CreateBranch(name, steamBranch, installPath string) (int64, error) {
	result, err := db.Exec(
		"INSERT INTO arma_branches (name, steam_branch, install_path) VALUES (?, ?, ?)",
		name, steamBranch, installPath,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func SetBranchInstalled(id int, installed bool) error {
	_, err := db.Exec(
		"UPDATE arma_branches SET installed = ?, last_updated = CURRENT_TIMESTAMP WHERE id = ?",
		installed, id,
	)
	return err
}

func GetServers() ([]Server, error) {
	rows, err := db.Query(`
		SELECT id, name, slug, branch_id, modset_id, game_port, steam_port, profile_name,
		       COALESCE(extra_launch_params, ''), status, pid, COALESCE(active_rpt, ''),
		       created_at, updated_at
		FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServers(rows)
}

func GetServer(id int) (*Server, error) {
	rows, err := db.Query(`
		SELECT id, name, slug, branch_id, modset_id, game_port, steam_port, profile_name,
		       COALESCE(extra_launch_params, ''), status, pid, COALESCE(active_rpt, ''),
		       created_at, updated_at
		FROM servers WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	servers, err := scanServers(rows)
	if err != nil || len(servers) == 0 {
		if err == nil {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return &servers[0], nil
}

func scanServers(rows *sql.Rows) ([]Server, error) {
	var servers []Server
	for rows.Next() {
		var s Server
		var modsetID sql.NullInt64
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Slug, &s.BranchID, &modsetID, &s.GamePort, &s.SteamPort,
			&s.ProfileName, &s.ExtraLaunchParams, &s.Status, &s.PID, &s.ActiveRPT,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if modsetID.Valid {
			v := int(modsetID.Int64)
			s.ModsetID = &v
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

func CreateServer(name, slug string, branchID int, modsetID *int, gamePort, steamPort int, profileName string) (int64, error) {
	if profileName == "" {
		profileName = "Server"
	}
	result, err := db.Exec(`
		INSERT INTO servers (name, slug, branch_id, modset_id, game_port, steam_port, profile_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, slug, branchID, modsetID, gamePort, steamPort, profileName,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateServer(id int, name string, branchID int, modsetID *int, gamePort, steamPort int, profileName, extraLaunchParams string) error {
	_, err := db.Exec(`
		UPDATE servers SET name = ?, branch_id = ?, modset_id = ?, game_port = ?, steam_port = ?,
			profile_name = ?, extra_launch_params = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		name, branchID, modsetID, gamePort, steamPort, profileName, extraLaunchParams, id,
	)
	return err
}

func UpdateServerStatus(id int, status string, pid int, activeRPT string) error {
	_, err := db.Exec(`
		UPDATE servers SET status = ?, pid = ?, active_rpt = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, status, pid, activeRPT, id)
	return err
}

func DeleteServer(id int) error {
	_, err := db.Exec("DELETE FROM servers WHERE id = ?", id)
	return err
}

func GetServerMods(serverID int) ([]ServerMod, error) {
	rows, err := db.Query(`
		SELECT id, server_id, mod_id, COALESCE(custom_path, ''), label, is_server_mod, sort_order
		FROM server_mods WHERE server_id = ? ORDER BY sort_order, id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []ServerMod
	for rows.Next() {
		var m ServerMod
		var modID sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ServerID, &modID, &m.CustomPath, &m.Label, &m.IsServerMod, &m.SortOrder); err != nil {
			return nil, err
		}
		if modID.Valid {
			v := int(modID.Int64)
			m.ModID = &v
		}
		mods = append(mods, m)
	}
	return mods, rows.Err()
}

func ReplaceServerMods(serverID int, mods []ServerMod) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM server_mods WHERE server_id = ?", serverID); err != nil {
		return err
	}
	for i, m := range mods {
		var modID interface{}
		if m.ModID != nil {
			modID = *m.ModID
		}
		_, err := tx.Exec(`
			INSERT INTO server_mods (server_id, mod_id, custom_path, label, is_server_mod, sort_order)
			VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, modID, nullIfEmpty(m.CustomPath), m.Label, m.IsServerMod, i,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SeedServerModsFromModset(serverID, modsetID int) error {
	mods, err := GetMods(modsetID)
	if err != nil {
		return err
	}
	var serverMods []ServerMod
	for i, m := range mods {
		modID := m.ID
		serverMods = append(serverMods, ServerMod{
			ServerID:    serverID,
			ModID:       &modID,
			Label:       m.Name,
			IsServerMod: false,
			SortOrder:   i,
		})
	}
	return ReplaceServerMods(serverID, serverMods)
}

func GetServerKeys(serverID int) ([]ServerKey, error) {
	rows, err := db.Query(
		"SELECT id, server_id, filename, source_path FROM server_keys WHERE server_id = ? ORDER BY filename",
		serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []ServerKey
	for rows.Next() {
		var k ServerKey
		if err := rows.Scan(&k.ID, &k.ServerID, &k.Filename, &k.SourcePath); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func AddServerKey(serverID int, filename, sourcePath string) (int64, error) {
	result, err := db.Exec(
		"INSERT INTO server_keys (server_id, filename, source_path) VALUES (?, ?, ?)",
		serverID, filename, sourcePath,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func DeleteServerKey(id int) error {
	_, err := db.Exec("DELETE FROM server_keys WHERE id = ?", id)
	return err
}

func GetServerKey(id int) (*ServerKey, error) {
	var k ServerKey
	err := db.QueryRow(
		"SELECT id, server_id, filename, source_path FROM server_keys WHERE id = ?", id,
	).Scan(&k.ID, &k.ServerID, &k.Filename, &k.SourcePath)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func GetServersUsingPort(gamePort, steamPort int, excludeID int) ([]Server, error) {
	rows, err := db.Query(`
		SELECT id, name, slug, branch_id, modset_id, game_port, steam_port, profile_name,
		       COALESCE(extra_launch_params, ''), status, pid, COALESCE(active_rpt, ''),
		       created_at, updated_at
		FROM servers WHERE id != ? AND (game_port = ? OR steam_port = ? OR game_port = ? OR steam_port = ?)`,
		excludeID, gamePort, steamPort, steamPort, gamePort,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServers(rows)
}

func ReconcileServerStatuses() error {
	servers, err := GetServers()
	if err != nil {
		return err
	}
	for _, s := range servers {
		if s.Status == ServerStatusRunning || s.Status == ServerStatusStarting {
			if s.PID <= 0 || !processExists(s.PID) {
				_ = UpdateServerStatus(s.ID, ServerStatusUnknown, 0, s.ActiveRPT)
			}
		}
	}
	return nil
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
