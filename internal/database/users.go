package database

import (
	"database/sql"
	"fmt"
	"time"
)

const (
	SubjectPasswordAdmin = "password_admin"
	SubjectUser          = "user"
)

type User struct {
	ID                 int       `json:"id"`
	SteamID            string    `json:"steamId"`
	DisplayName        string    `json:"displayName"`
	AvatarURL          string    `json:"avatarUrl"`
	IsFullAdmin        bool      `json:"isFullAdmin"`
	CanManageMods      bool      `json:"canManageMods"`
	CanManageBranches  bool      `json:"canManageBranches"`
	ServerIDs          []int     `json:"serverIds"`
	CreatedAt          time.Time `json:"createdAt"`
	LastLoginAt        time.Time `json:"lastLoginAt"`
}

type SessionRow struct {
	Token       string
	SubjectType string
	UserID      sql.NullInt64
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

func migrateUsers() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			steam_id TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			is_full_admin BOOLEAN DEFAULT 0,
			can_manage_mods BOOLEAN DEFAULT 0,
			can_manage_branches BOOLEAN DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_login_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_admins (
			user_id INTEGER NOT NULL,
			server_id INTEGER NOT NULL,
			PRIMARY KEY (user_id, server_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			subject_type TEXT NOT NULL,
			user_id INTEGER,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_server_admins_user ON server_admins(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_server_admins_server ON server_admins(server_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func UpsertSteamUser(steamID, displayName, avatarURL string) (*User, error) {
	now := time.Now()
	existing, err := GetUserBySteamID(steamID)
	if err == nil && existing != nil {
		_, err = db.Exec(
			`UPDATE users SET display_name = ?, avatar_url = ?, last_login_at = ? WHERE id = ?`,
			displayName, avatarURL, now, existing.ID,
		)
		if err != nil {
			return nil, err
		}
		existing.DisplayName = displayName
		existing.AvatarURL = avatarURL
		existing.LastLoginAt = now
		return existing, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	res, err := db.Exec(
		`INSERT INTO users (steam_id, display_name, avatar_url, last_login_at) VALUES (?, ?, ?, ?)`,
		steamID, displayName, avatarURL, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return GetUser(int(id))
}

func GetUser(id int) (*User, error) {
	u := &User{}
	var lastLogin sql.NullTime
	err := db.QueryRow(
		`SELECT id, steam_id, display_name, avatar_url, is_full_admin, can_manage_mods, can_manage_branches, created_at, last_login_at
		 FROM users WHERE id = ?`, id,
	).Scan(
		&u.ID, &u.SteamID, &u.DisplayName, &u.AvatarURL,
		&u.IsFullAdmin, &u.CanManageMods, &u.CanManageBranches,
		&u.CreatedAt, &lastLogin,
	)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	ids, err := GetUserServerIDs(u.ID)
	if err != nil {
		return nil, err
	}
	u.ServerIDs = ids
	return u, nil
}

func GetUserBySteamID(steamID string) (*User, error) {
	u := &User{}
	var lastLogin sql.NullTime
	err := db.QueryRow(
		`SELECT id, steam_id, display_name, avatar_url, is_full_admin, can_manage_mods, can_manage_branches, created_at, last_login_at
		 FROM users WHERE steam_id = ?`, steamID,
	).Scan(
		&u.ID, &u.SteamID, &u.DisplayName, &u.AvatarURL,
		&u.IsFullAdmin, &u.CanManageMods, &u.CanManageBranches,
		&u.CreatedAt, &lastLogin,
	)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	ids, err := GetUserServerIDs(u.ID)
	if err != nil {
		return nil, err
	}
	u.ServerIDs = ids
	return u, nil
}

func ListUsers() ([]User, error) {
	rows, err := db.Query(
		`SELECT id, steam_id, display_name, avatar_url, is_full_admin, can_manage_mods, can_manage_branches, created_at, last_login_at
		 FROM users ORDER BY display_name COLLATE NOCASE, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var lastLogin sql.NullTime
		if err := rows.Scan(
			&u.ID, &u.SteamID, &u.DisplayName, &u.AvatarURL,
			&u.IsFullAdmin, &u.CanManageMods, &u.CanManageBranches,
			&u.CreatedAt, &lastLogin,
		); err != nil {
			return nil, err
		}
		if lastLogin.Valid {
			u.LastLoginAt = lastLogin.Time
		}
		u.ServerIDs = []int{}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		return []User{}, nil
	}

	// Load server assignments after closing the users cursor — required with
	// MaxOpenConns(1), which deadlocks on nested queries while rows are open.
	adminRows, err := db.Query(`SELECT user_id, server_id FROM server_admins ORDER BY user_id, server_id`)
	if err != nil {
		return nil, err
	}
	defer adminRows.Close()

	byUser := make(map[int][]int, len(out))
	for adminRows.Next() {
		var userID, serverID int
		if err := adminRows.Scan(&userID, &serverID); err != nil {
			return nil, err
		}
		byUser[userID] = append(byUser[userID], serverID)
	}
	if err := adminRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if ids, ok := byUser[out[i].ID]; ok {
			out[i].ServerIDs = ids
		}
	}
	return out, nil
}

func UpdateUserPermissions(id int, isFullAdmin, canManageMods, canManageBranches bool, serverIDs []int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE users SET is_full_admin = ?, can_manage_mods = ?, can_manage_branches = ? WHERE id = ?`,
		isFullAdmin, canManageMods, canManageBranches, id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}

	if _, err := tx.Exec(`DELETE FROM server_admins WHERE user_id = ?`, id); err != nil {
		return err
	}
	for _, sid := range serverIDs {
		if _, err := tx.Exec(`INSERT INTO server_admins (user_id, server_id) VALUES (?, ?)`, id, sid); err != nil {
			return fmt.Errorf("assign server %d: %w", sid, err)
		}
	}
	return tx.Commit()
}

func DeleteUser(id int) error {
	res, err := db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func GetUserServerIDs(userID int) ([]int, error) {
	rows, err := db.Query(`SELECT server_id FROM server_admins WHERE user_id = ? ORDER BY server_id`, userID)
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
	if ids == nil {
		ids = []int{}
	}
	return ids, rows.Err()
}

func CreateSession(token, subjectType string, userID *int, expiresAt time.Time) error {
	var uid interface{}
	if userID != nil {
		uid = *userID
	}
	_, err := db.Exec(
		`INSERT INTO sessions (token, subject_type, user_id, expires_at) VALUES (?, ?, ?, ?)`,
		token, subjectType, uid, expiresAt,
	)
	return err
}

func GetSession(token string) (*SessionRow, error) {
	s := &SessionRow{}
	err := db.QueryRow(
		`SELECT token, subject_type, user_id, expires_at, created_at FROM sessions WHERE token = ?`,
		token,
	).Scan(&s.Token, &s.SubjectType, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func TouchSession(token string, expiresAt time.Time) error {
	_, err := db.Exec(`UPDATE sessions SET expires_at = ? WHERE token = ?`, expiresAt, token)
	return err
}

func DeleteSession(token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func DeleteSessionsForUser(userID int) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func CleanupExpiredSessions() error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
	return err
}
