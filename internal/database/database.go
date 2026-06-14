package database

import (
	"database/sql"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type Config struct {
	ID                  int    `json:"id"`
	Username            string `json:"username"`
	Password            string `json:"-"`
	ApiKey              string `json:"apiKey"`
	Downloader          string `json:"downloader"`          // "depotdownloader" | "steamcmd"
	SteamCmdPath        string `json:"steamCmdPath"`        // optional; blank = use PATH
	DepotDownloaderPath string `json:"depotDownloaderPath"` // optional; blank = use PATH
}

type Modset struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Mod struct {
	ID              int       `json:"id"`
	WorkshopID      string    `json:"workshopId"`
	Name            string    `json:"name"`
	Downloaded      bool      `json:"downloaded"`
	LocalPath       string    `json:"localPath"`
	LastUpdated     time.Time `json:"lastUpdated"`
	LastChecked     time.Time `json:"lastChecked"`
	UpdateAvailable bool      `json:"updateAvailable"`
}

func Init() error {
	var err error
	db, err = sql.Open("sqlite3", "./arma3mods.db")
	if err != nil {
		return err
	}

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		password TEXT NOT NULL,
		api_key TEXT,
		downloader TEXT DEFAULT 'depotdownloader',
		steam_cmd_path TEXT DEFAULT '',
		depot_downloader_path TEXT DEFAULT ''
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
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return migrateServers()
}

func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func GetConfig() (*Config, error) {
	config := &Config{}
	err := db.QueryRow(`
		SELECT id, username, password, COALESCE(api_key, ''),
		       COALESCE(downloader, 'depotdownloader'),
		       COALESCE(steam_cmd_path, ''),
		       COALESCE(depot_downloader_path, '')
		FROM config LIMIT 1`).
		Scan(&config.ID, &config.Username, &config.Password, &config.ApiKey,
			&config.Downloader, &config.SteamCmdPath, &config.DepotDownloaderPath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return config, err
}

func SaveConfig(username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath string) error {
	config, err := GetConfig()
	if err != nil {
		return err
	}

	if config == nil {
		_, err = db.Exec(
			"INSERT INTO config (username, password, api_key, downloader, steam_cmd_path, depot_downloader_path) VALUES (?, ?, ?, ?, ?, ?)",
			username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath,
		)
	} else {
		_, err = db.Exec(
			"UPDATE config SET username = ?, password = ?, api_key = ?, downloader = ?, steam_cmd_path = ?, depot_downloader_path = ? WHERE id = ?",
			username, password, apiKey, downloader, steamCmdPath, depotDownloaderPath, config.ID,
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
		       COALESCE(m.last_updated, ''), COALESCE(m.last_checked, ''), m.update_available
		FROM mods m
		INNER JOIN modset_mods mm ON m.id = mm.mod_id
		WHERE mm.modset_id = ?
		ORDER BY m.name`, modsetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []Mod
	for rows.Next() {
		var m Mod
		var lastUpdated, lastChecked string
		if err := rows.Scan(&m.ID, &m.WorkshopID, &m.Name, &m.Downloaded,
			&m.LocalPath, &lastUpdated, &lastChecked, &m.UpdateAvailable); err != nil {
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

func GetAllMods() ([]Mod, error) {
	rows, err := db.Query(`
		SELECT id, workshop_id, name, downloaded, COALESCE(local_path, ''), 
		       COALESCE(last_updated, ''), COALESCE(last_checked, ''), update_available
		FROM mods 
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mods []Mod
	for rows.Next() {
		var m Mod
		var lastUpdated, lastChecked string
		if err := rows.Scan(&m.ID, &m.WorkshopID, &m.Name, &m.Downloaded,
			&m.LocalPath, &lastUpdated, &lastChecked, &m.UpdateAvailable); err != nil {
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

func DeleteMod(id int) error {
	_, err := db.Exec("DELETE FROM mods WHERE id = ?", id)
	return err
}

func GetMod(id int) (*Mod, error) {
	var mod Mod
	var lastUpdated, lastChecked sql.NullTime
	var localPath sql.NullString

	err := db.QueryRow(
		"SELECT id, workshop_id, name, downloaded, local_path, last_updated, last_checked, update_available FROM mods WHERE id = ?",
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
	)
	if err != nil {
		return nil, err
	}

	// Handle NULL values
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
