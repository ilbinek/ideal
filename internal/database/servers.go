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

	ServerModSourceCustom = "custom"
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
	ServerBinary      string    `json:"serverBinary"`
	ExtraLaunchParams string    `json:"extraLaunchParams"`
	EnabledCdlcs      []string  `json:"enabledCdlcs"`
	HeadlessCount     int       `json:"headlessCount"`
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
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"`
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
			server_binary TEXT DEFAULT '',
			local_mods_root TEXT DEFAULT '',
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
			enabled BOOLEAN DEFAULT 1,
			source TEXT DEFAULT '',
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
	if err := ensureCreatorDlcBranch(); err != nil {
		return err
	}
	if err := removeInvalidCreativeDevBranch(); err != nil {
		return err
	}
	if err := fixPerformanceBranchCode(); err != nil {
		return err
	}
	if err := migrateBranchInstallPaths(); err != nil {
		return err
	}
	if err := migrateServerColumns(); err != nil {
		return err
	}
	return migrateHeadless()
}

func migrateServerColumns() error {
	alters := []string{
		"ALTER TABLE servers ADD COLUMN server_binary TEXT DEFAULT ''",
		"ALTER TABLE servers ADD COLUMN local_mods_root TEXT DEFAULT ''",
		"ALTER TABLE servers ADD COLUMN headless_count INTEGER DEFAULT 0",
		"ALTER TABLE servers ADD COLUMN enabled_cdlcs TEXT DEFAULT ''",
		"ALTER TABLE server_mods ADD COLUMN enabled BOOLEAN DEFAULT 1",
		"ALTER TABLE server_mods ADD COLUMN source TEXT DEFAULT ''",
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return nil
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
	case "Creator DLC":
		return "creatordlc"
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
		{"Creator DLC", "creatordlc", "creatordlc"},
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

// ensureCreatorDlcBranch inserts the Creator DLC branch for existing databases
// that were seeded before it was a default.
func ensureCreatorDlcBranch() error {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM arma_branches WHERE steam_branch = 'creatordlc' OR name = 'Creator DLC'`,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	path := utils.BranchInstallPath("creatordlc")
	_, err := db.Exec(
		"INSERT INTO arma_branches (name, steam_branch, install_path) VALUES (?, ?, ?)",
		"Creator DLC", "creatordlc", path,
	)
	return err
}

// removeInvalidCreativeDevBranch drops the legacy Creative Dev seed when unused.
// creativedev is not a valid Arma 3 Server Steam beta (use creatordlc instead).
func removeInvalidCreativeDevBranch() error {
	var id int
	err := db.QueryRow(
		`SELECT id FROM arma_branches WHERE steam_branch = 'creativedev' OR name = 'Creative Dev' LIMIT 1`,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var servers int
	if err := db.QueryRow("SELECT COUNT(*) FROM servers WHERE branch_id = ?", id).Scan(&servers); err != nil {
		return err
	}
	if servers > 0 {
		return nil
	}
	_, err = db.Exec("DELETE FROM arma_branches WHERE id = ?", id)
	return err
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

// GetBranchBySteamBranch returns the first branch with the given Steam beta name.
func GetBranchBySteamBranch(steamBranch string) (*ArmaBranch, error) {
	var b ArmaBranch
	var lastUpdated sql.NullTime
	err := db.QueryRow(`
		SELECT id, name, steam_branch, install_path, installed, last_updated
		FROM arma_branches WHERE steam_branch = ? ORDER BY id LIMIT 1`, steamBranch).
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
		       COALESCE(server_binary, ''), COALESCE(extra_launch_params, ''),
		       COALESCE(enabled_cdlcs, ''),
		       COALESCE(headless_count, 0),
		       status, pid, COALESCE(active_rpt, ''), created_at, updated_at
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
		       COALESCE(server_binary, ''), COALESCE(extra_launch_params, ''),
		       COALESCE(enabled_cdlcs, ''),
		       COALESCE(headless_count, 0),
		       status, pid, COALESCE(active_rpt, ''), created_at, updated_at
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
		var enabledCdlcs string
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Slug, &s.BranchID, &modsetID, &s.GamePort, &s.SteamPort,
			&s.ProfileName, &s.ServerBinary, &s.ExtraLaunchParams, &enabledCdlcs, &s.HeadlessCount,
			&s.Status, &s.PID, &s.ActiveRPT, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if modsetID.Valid {
			v := int(modsetID.Int64)
			s.ModsetID = &v
		}
		s.EnabledCdlcs = DecodeCdlcs(enabledCdlcs)
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

func CreateServer(name, slug string, branchID int, modsetID *int, gamePort, steamPort int, profileName, serverBinary string) (int64, error) {
	if profileName == "" {
		profileName = "Server"
	}
	result, err := db.Exec(`
		INSERT INTO servers (name, slug, branch_id, modset_id, game_port, steam_port, profile_name, server_binary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, slug, branchID, modsetID, gamePort, steamPort, profileName,
		strings.TrimSpace(serverBinary),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateServer(id int, name string, branchID int, modsetID *int, gamePort, steamPort int, profileName, serverBinary, extraLaunchParams string, enabledCdlcs []string) error {
	srv, err := GetServer(id)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		UPDATE servers SET name = ?, branch_id = ?, modset_id = ?, game_port = ?, steam_port = ?,
			profile_name = ?, server_binary = ?, extra_launch_params = ?, enabled_cdlcs = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		name, branchID, modsetID, gamePort, steamPort, profileName,
		strings.TrimSpace(serverBinary), extraLaunchParams, EncodeCdlcs(enabledCdlcs), id,
	)
	if err != nil {
		return err
	}
	if name != srv.Name {
		return RefreshHeadlessProfileNames(id, name, srv.Slug)
	}
	return nil
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
		SELECT id, server_id, mod_id, COALESCE(custom_path, ''), label, is_server_mod,
		       COALESCE(enabled, 1), COALESCE(source, ''), sort_order
		FROM server_mods WHERE server_id = ? ORDER BY sort_order, id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []ServerMod
	for rows.Next() {
		var m ServerMod
		var modID sql.NullInt64
		var enabled int
		if err := rows.Scan(&m.ID, &m.ServerID, &modID, &m.CustomPath, &m.Label, &m.IsServerMod, &enabled, &m.Source, &m.SortOrder); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		if modID.Valid {
			v := int(modID.Int64)
			m.ModID = &v
		}
		if m.Source == "" || m.Source == "local_root" {
			if m.ModID == nil && m.CustomPath != "" {
				m.Source = ServerModSourceCustom
			}
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
		source := m.Source
		if source == "" || source == "local_root" {
			if m.ModID == nil && m.CustomPath != "" {
				source = ServerModSourceCustom
			} else {
				source = ""
			}
		}
		enabled := 0
		if m.Enabled {
			enabled = 1
		}
		_, err := tx.Exec(`
			INSERT INTO server_mods (server_id, mod_id, custom_path, label, is_server_mod, enabled, source, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			serverID, modID, nullIfEmpty(m.CustomPath), m.Label, m.IsServerMod, enabled, source, i,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SeedServerModsFromModset(serverID, modsetID int) error {
	existing, err := GetServerMods(serverID)
	if err != nil {
		return err
	}
	var preserved []ServerMod
	for _, m := range existing {
		// Keep manually added path-based mods when swapping modsets.
		if m.ModID == nil && m.CustomPath != "" {
			preserved = append(preserved, m)
		}
	}

	mods, err := GetMods(modsetID)
	if err != nil {
		return err
	}
	var serverMods []ServerMod
	serverMods = append(serverMods, preserved...)
	for i, m := range mods {
		modID := m.ID
		serverMods = append(serverMods, ServerMod{
			ServerID:    serverID,
			ModID:       &modID,
			Label:       m.Name,
			IsServerMod: false,
			Enabled:     true,
			SortOrder:   len(preserved) + i,
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

// GetServerIDsUsingMod returns distinct server IDs that have the given mod assigned.
func GetServerIDsUsingMod(modID int) ([]int, error) {
	rows, err := db.Query(
		`SELECT DISTINCT server_id FROM server_mods WHERE mod_id = ? ORDER BY server_id`,
		modID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ModServerRef is a lightweight server reference for mod usage panels.
type ModServerRef struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// GetServersUsingMod returns servers that have the given workshop mod assigned.
func GetServersUsingMod(modID int) ([]ModServerRef, error) {
	rows, err := db.Query(`
		SELECT DISTINCT s.id, s.name, s.slug, s.status
		FROM servers s
		INNER JOIN server_mods sm ON sm.server_id = s.id
		WHERE sm.mod_id = ?
		ORDER BY s.name`, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []ModServerRef
	for rows.Next() {
		var s ModServerRef
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Status); err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	if servers == nil {
		servers = []ModServerRef{}
	}
	return servers, rows.Err()
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
		       COALESCE(server_binary, ''), COALESCE(extra_launch_params, ''),
		       COALESCE(enabled_cdlcs, ''),
		       COALESCE(headless_count, 0),
		       status, pid, COALESCE(active_rpt, ''), created_at, updated_at
		FROM servers WHERE id != ? AND (game_port = ? OR steam_port = ? OR game_port = ? OR steam_port = ?)`,
		excludeID, gamePort, steamPort, steamPort, gamePort,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServers(rows)
}

// EncodeCdlcs serializes cDLC folder codes as a semicolon-separated list.
func EncodeCdlcs(codes []string) string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range codes {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return strings.Join(out, ";")
}

// DecodeCdlcs parses a semicolon-separated cDLC code list.
func DecodeCdlcs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ";")
	seen := make(map[string]bool)
	var out []string
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
