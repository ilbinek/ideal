package database

import (
	"database/sql"
	"fmt"
	"time"
)

const (
	ContentKindMod    = "mod"
	ContentKindBranch = "branch"
)

// ModGeneration is one on-disk generation of a workshop mod.
type ModGeneration struct {
	ModID      int       `json:"modId"`
	Generation int       `json:"generation"`
	Path       string    `json:"path"`
	IsCurrent  bool      `json:"isCurrent"`
	CreatedAt  time.Time `json:"createdAt"`
}

// BranchGeneration is one on-disk generation of a dedicated-server branch.
type BranchGeneration struct {
	BranchID   int       `json:"branchId"`
	Generation int       `json:"generation"`
	Path       string    `json:"path"`
	IsCurrent  bool      `json:"isCurrent"`
	BuildID    string    `json:"buildId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ServerContentPin records which generation a running server is using.
type ServerContentPin struct {
	ServerID   int    `json:"serverId"`
	Kind       string `json:"kind"`
	RefID      int    `json:"refId"`
	Generation int    `json:"generation"`
}

func migrateGenerations() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mod_generations (
			mod_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			path TEXT NOT NULL,
			is_current BOOLEAN DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (mod_id, generation),
			FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS branch_generations (
			branch_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			path TEXT NOT NULL,
			is_current BOOLEAN DEFAULT 0,
			build_id TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (branch_id, generation),
			FOREIGN KEY (branch_id) REFERENCES arma_branches(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS server_content_pins (
			server_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			ref_id INTEGER NOT NULL,
			generation INTEGER NOT NULL,
			PRIMARY KEY (server_id, kind, ref_id),
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mod_generations_current ON mod_generations(mod_id, is_current)`,
		`CREATE INDEX IF NOT EXISTS idx_branch_generations_current ON branch_generations(branch_id, is_current)`,
		`CREATE INDEX IF NOT EXISTS idx_server_content_pins_ref ON server_content_pins(kind, ref_id, generation)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// --- Mod generations ---

func GetCurrentModGeneration(modID int) (*ModGeneration, error) {
	var g ModGeneration
	var created string
	err := db.QueryRow(`
		SELECT mod_id, generation, path, is_current, COALESCE(created_at, '')
		FROM mod_generations WHERE mod_id = ? AND is_current = 1`, modID).
		Scan(&g.ModID, &g.Generation, &g.Path, &g.IsCurrent, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created != "" {
		g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	}
	return &g, nil
}

func GetModGeneration(modID, generation int) (*ModGeneration, error) {
	var g ModGeneration
	var created string
	err := db.QueryRow(`
		SELECT mod_id, generation, path, is_current, COALESCE(created_at, '')
		FROM mod_generations WHERE mod_id = ? AND generation = ?`, modID, generation).
		Scan(&g.ModID, &g.Generation, &g.Path, &g.IsCurrent, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created != "" {
		g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	}
	return &g, nil
}

func ListModGenerations(modID int) ([]ModGeneration, error) {
	rows, err := db.Query(`
		SELECT mod_id, generation, path, is_current, COALESCE(created_at, '')
		FROM mod_generations WHERE mod_id = ? ORDER BY generation`, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModGeneration
	for rows.Next() {
		var g ModGeneration
		var created string
		if err := rows.Scan(&g.ModID, &g.Generation, &g.Path, &g.IsCurrent, &created); err != nil {
			return nil, err
		}
		if created != "" {
			g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func MaxModGeneration(modID int) (int, error) {
	var max sql.NullInt64
	err := db.QueryRow(`SELECT MAX(generation) FROM mod_generations WHERE mod_id = ?`, modID).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

func UpsertModGeneration(modID, generation int, path string, isCurrent bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if isCurrent {
		if _, err := tx.Exec(`UPDATE mod_generations SET is_current = 0 WHERE mod_id = ?`, modID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`
		INSERT INTO mod_generations (mod_id, generation, path, is_current)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(mod_id, generation) DO UPDATE SET
			path = excluded.path,
			is_current = excluded.is_current`,
		modID, generation, path, isCurrent)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func SetCurrentModGeneration(modID, generation int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE mod_generations SET is_current = 0 WHERE mod_id = ?`, modID); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE mod_generations SET is_current = 1 WHERE mod_id = ? AND generation = ?`, modID, generation)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mod %d generation %d not found", modID, generation)
	}
	return tx.Commit()
}

func DeleteModGeneration(modID, generation int) error {
	_, err := db.Exec(`DELETE FROM mod_generations WHERE mod_id = ? AND generation = ?`, modID, generation)
	return err
}

func DeleteAllModGenerations(modID int) error {
	_, err := db.Exec(`DELETE FROM mod_generations WHERE mod_id = ?`, modID)
	return err
}

// ListOrphanModGenerations returns non-current mod gens with zero pin refs.
func ListOrphanModGenerations() ([]ModGeneration, error) {
	rows, err := db.Query(`
		SELECT mg.mod_id, mg.generation, mg.path, mg.is_current, COALESCE(mg.created_at, '')
		FROM mod_generations mg
		WHERE mg.is_current = 0
		  AND NOT EXISTS (
			SELECT 1 FROM server_content_pins p
			WHERE p.kind = ? AND p.ref_id = mg.mod_id AND p.generation = mg.generation
		  )`, ContentKindMod)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanModGenerations(rows)
}

func scanModGenerations(rows *sql.Rows) ([]ModGeneration, error) {
	var out []ModGeneration
	for rows.Next() {
		var g ModGeneration
		var created string
		if err := rows.Scan(&g.ModID, &g.Generation, &g.Path, &g.IsCurrent, &created); err != nil {
			return nil, err
		}
		if created != "" {
			g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// --- Branch generations ---

func GetCurrentBranchGeneration(branchID int) (*BranchGeneration, error) {
	var g BranchGeneration
	var created string
	err := db.QueryRow(`
		SELECT branch_id, generation, path, is_current, COALESCE(build_id, ''), COALESCE(created_at, '')
		FROM branch_generations WHERE branch_id = ? AND is_current = 1`, branchID).
		Scan(&g.BranchID, &g.Generation, &g.Path, &g.IsCurrent, &g.BuildID, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created != "" {
		g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	}
	return &g, nil
}

func GetBranchGeneration(branchID, generation int) (*BranchGeneration, error) {
	var g BranchGeneration
	var created string
	err := db.QueryRow(`
		SELECT branch_id, generation, path, is_current, COALESCE(build_id, ''), COALESCE(created_at, '')
		FROM branch_generations WHERE branch_id = ? AND generation = ?`, branchID, generation).
		Scan(&g.BranchID, &g.Generation, &g.Path, &g.IsCurrent, &g.BuildID, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created != "" {
		g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	}
	return &g, nil
}

func ListBranchGenerations(branchID int) ([]BranchGeneration, error) {
	rows, err := db.Query(`
		SELECT branch_id, generation, path, is_current, COALESCE(build_id, ''), COALESCE(created_at, '')
		FROM branch_generations WHERE branch_id = ? ORDER BY generation`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BranchGeneration
	for rows.Next() {
		var g BranchGeneration
		var created string
		if err := rows.Scan(&g.BranchID, &g.Generation, &g.Path, &g.IsCurrent, &g.BuildID, &created); err != nil {
			return nil, err
		}
		if created != "" {
			g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func MaxBranchGeneration(branchID int) (int, error) {
	var max sql.NullInt64
	err := db.QueryRow(`SELECT MAX(generation) FROM branch_generations WHERE branch_id = ?`, branchID).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

func UpsertBranchGeneration(branchID, generation int, path, buildID string, isCurrent bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if isCurrent {
		if _, err := tx.Exec(`UPDATE branch_generations SET is_current = 0 WHERE branch_id = ?`, branchID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`
		INSERT INTO branch_generations (branch_id, generation, path, is_current, build_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(branch_id, generation) DO UPDATE SET
			path = excluded.path,
			is_current = excluded.is_current,
			build_id = excluded.build_id`,
		branchID, generation, path, isCurrent, buildID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func SetCurrentBranchGeneration(branchID, generation int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE branch_generations SET is_current = 0 WHERE branch_id = ?`, branchID); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE branch_generations SET is_current = 1 WHERE branch_id = ? AND generation = ?`, branchID, generation)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("branch %d generation %d not found", branchID, generation)
	}
	return tx.Commit()
}

func UpdateBranchGenerationBuildID(branchID, generation int, buildID string) error {
	_, err := db.Exec(
		`UPDATE branch_generations SET build_id = ? WHERE branch_id = ? AND generation = ?`,
		buildID, branchID, generation,
	)
	return err
}

func DeleteBranchGeneration(branchID, generation int) error {
	_, err := db.Exec(`DELETE FROM branch_generations WHERE branch_id = ? AND generation = ?`, branchID, generation)
	return err
}

func DeleteAllBranchGenerations(branchID int) error {
	_, err := db.Exec(`DELETE FROM branch_generations WHERE branch_id = ?`, branchID)
	return err
}

func ListOrphanBranchGenerations() ([]BranchGeneration, error) {
	rows, err := db.Query(`
		SELECT bg.branch_id, bg.generation, bg.path, bg.is_current, COALESCE(bg.build_id, ''), COALESCE(bg.created_at, '')
		FROM branch_generations bg
		WHERE bg.is_current = 0
		  AND NOT EXISTS (
			SELECT 1 FROM server_content_pins p
			WHERE p.kind = ? AND p.ref_id = bg.branch_id AND p.generation = bg.generation
		  )`, ContentKindBranch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BranchGeneration
	for rows.Next() {
		var g BranchGeneration
		var created string
		if err := rows.Scan(&g.BranchID, &g.Generation, &g.Path, &g.IsCurrent, &g.BuildID, &created); err != nil {
			return nil, err
		}
		if created != "" {
			g.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GenerationRefCount returns how many servers are pinned to a generation.
func GenerationRefCount(kind string, refID, generation int) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM server_content_pins
		WHERE kind = ? AND ref_id = ? AND generation = ?`,
		kind, refID, generation).Scan(&n)
	return n, err
}

// --- Server content pins ---

func GetServerPins(serverID int) ([]ServerContentPin, error) {
	rows, err := db.Query(`
		SELECT server_id, kind, ref_id, generation
		FROM server_content_pins WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerContentPin
	for rows.Next() {
		var p ServerContentPin
		if err := rows.Scan(&p.ServerID, &p.Kind, &p.RefID, &p.Generation); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func GetServerPin(serverID int, kind string, refID int) (*ServerContentPin, error) {
	var p ServerContentPin
	err := db.QueryRow(`
		SELECT server_id, kind, ref_id, generation
		FROM server_content_pins WHERE server_id = ? AND kind = ? AND ref_id = ?`,
		serverID, kind, refID).
		Scan(&p.ServerID, &p.Kind, &p.RefID, &p.Generation)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func ReplaceServerPins(serverID int, pins []ServerContentPin) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM server_content_pins WHERE server_id = ?`, serverID); err != nil {
		return err
	}
	for _, p := range pins {
		if _, err := tx.Exec(`
			INSERT INTO server_content_pins (server_id, kind, ref_id, generation)
			VALUES (?, ?, ?, ?)`, serverID, p.Kind, p.RefID, p.Generation); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ClearServerPins(serverID int) ([]ServerContentPin, error) {
	pins, err := GetServerPins(serverID)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`DELETE FROM server_content_pins WHERE server_id = ?`, serverID)
	return pins, err
}

func UpsertServerPin(serverID int, kind string, refID, generation int) error {
	_, err := db.Exec(`
		INSERT INTO server_content_pins (server_id, kind, ref_id, generation)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(server_id, kind, ref_id) DO UPDATE SET generation = excluded.generation`,
		serverID, kind, refID, generation)
	return err
}
