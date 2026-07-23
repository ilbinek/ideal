package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"arma3modmanager/internal/utils"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type Config struct {
	ID                         int    `json:"id"`
	Username                   string `json:"username"`
	Password                   string `json:"-"`
	ApiKey                     string `json:"apiKey"`
	Downloader                 string `json:"downloader"`                 // "gosteam" | "depotdownloader" | "steamcmd"
	SteamCmdPath               string `json:"steamCmdPath"`               // optional; blank = use PATH
	DepotDownloaderPath        string `json:"depotDownloaderPath"`        // optional; blank = use PATH
	MaxConcurrentDownloads     int    `json:"maxConcurrentDownloads"`     // gosteam: mods downloaded at once
	UpdateCheckIntervalMinutes int    `json:"updateCheckIntervalMinutes"` // 0 = off
	DiscordWebhookURL          string `json:"discordWebhookUrl"`
	UseDocker                  bool   `json:"useDocker"` // Linux only: run servers in Docker
}

type Modset struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Mod struct {
	ID                  int       `json:"id"`
	WorkshopID          string    `json:"workshopId"`
	Name                string    `json:"name"`
	Downloaded          bool      `json:"downloaded"`
	LocalPath           string    `json:"localPath"`
	LastUpdated         time.Time `json:"lastUpdated"`
	LastChecked         time.Time `json:"lastChecked"`
	UpdateAvailable     bool      `json:"updateAvailable"`
	LastNotifiedUpdated int64     `json:"-"` // Steam time_updated last Discord-notified for
}

func Init() error {
	dbPath := filepath.Join(utils.DataRoot(), "arma3mods.db")
	dsn := dbPath + "?_busy_timeout=10000&_journal_mode=WAL"
	var err error
	db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		password TEXT NOT NULL,
		api_key TEXT,
		downloader TEXT DEFAULT 'gosteam',
		steam_cmd_path TEXT DEFAULT '',
		depot_downloader_path TEXT DEFAULT '',
		max_concurrent_downloads INTEGER DEFAULT 4
	);

	CREATE TABLE IF NOT EXISTS modsets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS mods (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workshop_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		downloaded BOOLEAN DEFAULT 0,
		local_path TEXT,
		last_updated TIMESTAMP,
		last_checked TIMESTAMP,
		update_available BOOLEAN DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS modset_mods (
		modset_id INTEGER NOT NULL,
		mod_id INTEGER NOT NULL,
		PRIMARY KEY (modset_id, mod_id),
		FOREIGN KEY (modset_id) REFERENCES modsets(id) ON DELETE CASCADE,
		FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_mods_workshop ON mods(workshop_id);
	CREATE INDEX IF NOT EXISTS idx_modset_mods_modset ON modset_mods(modset_id);
	CREATE INDEX IF NOT EXISTS idx_modset_mods_mod ON modset_mods(mod_id);
	`

	if _, err = db.Exec(schema); err != nil {
		return err
	}

	return migrate()
}

// migrate adds columns introduced after the initial schema to pre-existing
// databases. SQLite has no "ADD COLUMN IF NOT EXISTS", so we run each ALTER and
// ignore the "duplicate column name" error when the column already exists.
func migrate() error {
	alters := []string{
		"ALTER TABLE config ADD COLUMN downloader TEXT DEFAULT 'depotdownloader'",
		"ALTER TABLE config ADD COLUMN steam_cmd_path TEXT DEFAULT ''",
		"ALTER TABLE config ADD COLUMN depot_downloader_path TEXT DEFAULT ''",
		"ALTER TABLE config ADD COLUMN max_concurrent_downloads INTEGER DEFAULT 4",
		"ALTER TABLE config ADD COLUMN update_check_interval_minutes INTEGER DEFAULT 0",
		"ALTER TABLE config ADD COLUMN discord_webhook_url TEXT DEFAULT ''",
		"ALTER TABLE config ADD COLUMN use_docker INTEGER DEFAULT 0",
		"ALTER TABLE mods ADD COLUMN last_notified_updated INTEGER DEFAULT 0",
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	if err := migrateServers(); err != nil {
		return err
	}
	if err := migrateGenerations(); err != nil {
		return err
	}
	return migrateUsers()
}

func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func GetConfig() (*Config, error) {
	config := &Config{}
	var useDocker int
	err := db.QueryRow(`
		SELECT id, username, password, COALESCE(api_key, ''),
		       COALESCE(downloader, 'gosteam'),
		       COALESCE(steam_cmd_path, ''),
		       COALESCE(depot_downloader_path, ''),
		       COALESCE(max_concurrent_downloads, 4),
		       COALESCE(update_check_interval_minutes, 0),
		       COALESCE(discord_webhook_url, ''),
		       COALESCE(use_docker, 0)
		FROM config LIMIT 1`).
		Scan(&config.ID, &config.Username, &config.Password, &config.ApiKey,
			&config.Downloader, &config.SteamCmdPath, &config.DepotDownloaderPath,
			&config.MaxConcurrentDownloads, &config.UpdateCheckIntervalMinutes,
			&config.DiscordWebhookURL, &useDocker)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	config.UseDocker = useDocker != 0
	return config, err
}

// ValidUpdateCheckIntervals are allowed auto-check presets (minutes); 0 = off.
var ValidUpdateCheckIntervals = map[int]bool{
	0: true, 15: true, 30: true, 60: true, 360: true, 720: true, 1440: true,
}

func SaveConfig(username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath string, maxConcurrentDownloads, updateCheckIntervalMinutes int, discordWebhookURL string, useDocker bool) error {
	config, err := GetConfig()
	if err != nil {
		return err
	}

	if maxConcurrentDownloads <= 0 {
		maxConcurrentDownloads = 4
	}
	if !ValidUpdateCheckIntervals[updateCheckIntervalMinutes] {
		updateCheckIntervalMinutes = 0
	}
	useDockerInt := 0
	if useDocker {
		useDockerInt = 1
	}

	if config == nil {
		_, err = db.Exec(
			`INSERT INTO config (username, password, api_key, downloader, steam_cmd_path, depot_downloader_path,
			 max_concurrent_downloads, update_check_interval_minutes, discord_webhook_url, use_docker)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath,
			maxConcurrentDownloads, updateCheckIntervalMinutes, discordWebhookURL, useDockerInt,
		)
	} else {
		_, err = db.Exec(
			`UPDATE config SET username = ?, password = ?, api_key = ?, downloader = ?, steam_cmd_path = ?,
			 depot_downloader_path = ?, max_concurrent_downloads = ?, update_check_interval_minutes = ?,
			 discord_webhook_url = ?, use_docker = ? WHERE id = ?`,
			username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath,
			maxConcurrentDownloads, updateCheckIntervalMinutes, discordWebhookURL, useDockerInt, config.ID,
		)
	}
	return err
}

func CreateModset(name string) (int64, error) {
	result, err := db.Exec("INSERT INTO modsets (name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func GetModsets() ([]Modset, error) {
	rows, err := db.Query("SELECT id, name, created_at FROM modsets ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modsets []Modset
	for rows.Next() {
		var m Modset
		if err := rows.Scan(&m.ID, &m.Name, &m.CreatedAt); err != nil {
			return nil, err
		}
		modsets = append(modsets, m)
	}
	return modsets, rows.Err()
}

func UpdateModset(id int, name string) error {
	result, err := db.Exec("UPDATE modsets SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func DeleteModset(id int) error {
	_, err := db.Exec("DELETE FROM modsets WHERE id = ?", id)
	return err
}

func AddMod(modsetID int, workshopID, name string) error {
	// First, try to get or create the mod
	var modID int64
	err := db.QueryRow("SELECT id FROM mods WHERE workshop_id = ?", workshopID).Scan(&modID)

	if err == sql.ErrNoRows {
		// Mod doesn't exist, create it
		result, err := db.Exec(
			"INSERT INTO mods (workshop_id, name) VALUES (?, ?)",
			workshopID, name,
		)
		if err != nil {
			return err
		}
		modID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Link mod to modset
	_, err = db.Exec(
		"INSERT OR IGNORE INTO modset_mods (modset_id, mod_id) VALUES (?, ?)",
		modsetID, modID,
	)
	return err
}

// CreateMod creates a mod without linking it to any modset
func CreateMod(workshopID, name string) error {
	// Check if mod already exists
	var existingID int64
	err := db.QueryRow("SELECT id FROM mods WHERE workshop_id = ?", workshopID).Scan(&existingID)

	if err == sql.ErrNoRows {
		// Mod doesn't exist, create it
		_, err := db.Exec(
			"INSERT INTO mods (workshop_id, name) VALUES (?, ?)",
			workshopID, name,
		)
		return err
	} else if err != nil {
		return err
	}

	// Mod already exists, no error
	return nil
}

func GetMods(modsetID int) ([]Mod, error) {
	rows, err := db.Query(`
		SELECT m.id, m.workshop_id, m.name, m.downloaded, COALESCE(m.local_path, ''), 
		       COALESCE(m.last_updated, ''), COALESCE(m.last_checked, ''), m.update_available,
		       COALESCE(m.last_notified_updated, 0)
		FROM mods m
		INNER JOIN modset_mods mm ON m.id = mm.mod_id
		WHERE mm.modset_id = ?
		ORDER BY m.name`, modsetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMods(rows)
}

func GetAllMods() ([]Mod, error) {
	rows, err := db.Query(`
		SELECT id, workshop_id, name, downloaded, COALESCE(local_path, ''), 
		       COALESCE(last_updated, ''), COALESCE(last_checked, ''), update_available,
		       COALESCE(last_notified_updated, 0)
		FROM mods 
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMods(rows)
}

func scanMods(rows *sql.Rows) ([]Mod, error) {
	var mods []Mod
	for rows.Next() {
		var m Mod
		var lastUpdated, lastChecked string
		if err := rows.Scan(&m.ID, &m.WorkshopID, &m.Name, &m.Downloaded,
			&m.LocalPath, &lastUpdated, &lastChecked, &m.UpdateAvailable, &m.LastNotifiedUpdated); err != nil {
			return nil, err
		}
		if lastUpdated != "" {
			m.LastUpdated, _ = time.Parse("2006-01-02 15:04:05", lastUpdated)
		}
		if lastChecked != "" {
			m.LastChecked, _ = time.Parse("2006-01-02 15:04:05", lastChecked)
		}
		mods = append(mods, m)
	}
	return mods, rows.Err()
}

func UpdateModDownloaded(id int, downloaded bool, localPath string) error {
	_, err := db.Exec(
		"UPDATE mods SET downloaded = ?, local_path = ?, last_updated = CURRENT_TIMESTAMP, update_available = 0 WHERE id = ?",
		downloaded, localPath, id,
	)
	return err
}

func UpdateModChecked(id int, updateAvailable bool) error {
	_, err := db.Exec(
		"UPDATE mods SET last_checked = CURRENT_TIMESTAMP, update_available = ? WHERE id = ?",
		updateAvailable, id,
	)
	return err
}

// SetModNotified records that Discord was notified for this Steam time_updated.
func SetModNotified(id int, steamTimeUpdated int64) error {
	_, err := db.Exec(
		"UPDATE mods SET last_notified_updated = ? WHERE id = ?",
		steamTimeUpdated, id,
	)
	return err
}

func DeleteMod(id int) error {
	_, err := db.Exec("DELETE FROM mods WHERE id = ?", id)
	return err
}

// RemoveModFromModset unlinks a mod from a modset without deleting the mod itself.
func RemoveModFromModset(modsetID, modID int) error {
	_, err := db.Exec(
		"DELETE FROM modset_mods WHERE modset_id = ? AND mod_id = ?",
		modsetID, modID,
	)
	return err
}

// GetModsByIDs returns mods matching the given IDs (order not guaranteed).
func GetModsByIDs(ids []int) ([]Mod, error) {
	if len(ids) == 0 {
		return []Mod{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT id, workshop_id, name, downloaded, COALESCE(local_path, ''),
		       COALESCE(last_updated, ''), COALESCE(last_checked, ''), update_available,
		       COALESCE(last_notified_updated, 0)
		FROM mods
		WHERE id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMods(rows)
}

func GetMod(id int) (*Mod, error) {
	var mod Mod
	var lastUpdated, lastChecked sql.NullTime
	var localPath sql.NullString

	err := db.QueryRow(
		`SELECT id, workshop_id, name, downloaded, local_path, last_updated, last_checked, update_available,
		        COALESCE(last_notified_updated, 0)
		 FROM mods WHERE id = ?`,
		id,
	).Scan(
		&mod.ID,
		&mod.WorkshopID,
		&mod.Name,
		&mod.Downloaded,
		&localPath,
		&lastUpdated,
		&lastChecked,
		&mod.UpdateAvailable,
		&mod.LastNotifiedUpdated,
	)
	if err != nil {
		return nil, err
	}

	if localPath.Valid {
		mod.LocalPath = localPath.String
	}
	if lastUpdated.Valid {
		mod.LastUpdated = lastUpdated.Time
	}
	if lastChecked.Valid {
		mod.LastChecked = lastChecked.Time
	}

	return &mod, nil
}

// GetModsetsForMod returns modsets that include the given mod.
func GetModsetsForMod(modID int) ([]Modset, error) {
	rows, err := db.Query(`
		SELECT ms.id, ms.name, ms.created_at
		FROM modsets ms
		INNER JOIN modset_mods mm ON ms.id = mm.modset_id
		WHERE mm.mod_id = ?
		ORDER BY ms.name`, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modsets []Modset
	for rows.Next() {
		var m Modset
		if err := rows.Scan(&m.ID, &m.Name, &m.CreatedAt); err != nil {
			return nil, err
		}
		modsets = append(modsets, m)
	}
	if modsets == nil {
		modsets = []Modset{}
	}
	return modsets, rows.Err()
}
