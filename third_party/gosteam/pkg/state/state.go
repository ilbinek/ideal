// Package state persists gosteam's cross-run data: Steam refresh tokens (so a
// password is only needed on first login) and the installed manifest id per
// depot (so the delta engine can diff against the previously downloaded state).
//
// This mirrors DepotDownloader's AccountSettingsStore (LoginTokens/GuardData)
// and DepotConfigStore (InstalledManifestIDs), but uses plain JSON files under a
// single state directory instead of protobuf+deflate in isolated storage.
package state

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
)

const (
	tokensFile    = "tokens.json"
	installedFile = "installed.json"
	// ManifestCacheDir holds cached manifest blobs ({depotID}_{manifestID}.manifest).
	ManifestCacheDir = "manifests"
	// StagingDir holds in-progress file writes before they are moved into place.
	StagingDir = "staging"
)

// Token holds the persisted credentials for a single Steam account.
type Token struct {
	RefreshToken string `json:"refresh_token"`
	GuardData    string `json:"guard_data,omitempty"`
}

// Store is a directory-backed store for tokens and installed manifest ids.
// All methods are safe for concurrent use.
type Store struct {
	dir string

	mu        sync.Mutex
	tokens    map[string]Token  // username -> token
	installed map[uint64]uint64 // workshop item (published file) id -> installed manifest id
}

// Open loads (or initializes) the state store rooted at dir, creating the
// directory and its manifest-cache/staging subdirectories if needed.
func Open(dir string) (*Store, error) {
	for _, sub := range []string{"", ManifestCacheDir, StagingDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("state: create %q: %w", sub, err)
		}
	}

	s := &Store{
		dir:       dir,
		tokens:    map[string]Token{},
		installed: map[uint64]uint64{},
	}
	if err := readJSON(filepath.Join(dir, tokensFile), &s.tokens); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, installedFile), &s.installed); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the root state directory.
func (s *Store) Dir() string { return s.dir }

// ManifestCachePath returns the on-disk path for a cached manifest blob.
func (s *Store) ManifestCachePath(depotID uint32, manifestID uint64) string {
	return filepath.Join(s.dir, ManifestCacheDir, fmt.Sprintf("%d_%d.manifest", depotID, manifestID))
}

// StagingPath returns a staging directory unique to a published file id.
func (s *Store) StagingPath(pubFileID uint64) string {
	return filepath.Join(s.dir, StagingDir, fmt.Sprintf("%d", pubFileID))
}

// Token returns the stored token for a username and whether one exists.
func (s *Store) Token(username string) (Token, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[username]
	return t, ok
}

// SaveToken stores the token for a username and flushes to disk.
func (s *Store) SaveToken(username string, t Token) error {
	s.mu.Lock()
	s.tokens[username] = t
	snapshot := cloneTokens(s.tokens)
	s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, tokensFile), snapshot)
}

// InstalledManifest returns the installed manifest id for a workshop item
// (published file id), or 0 if none.
func (s *Store) InstalledManifest(itemID uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installed[itemID]
}

// SetInstalledManifest records the installed manifest id for a workshop item and
// flushes to disk.
func (s *Store) SetInstalledManifest(itemID, manifestID uint64) error {
	s.mu.Lock()
	s.installed[itemID] = manifestID
	snapshot := cloneInstalled(s.installed)
	s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, installedFile), snapshot)
}

// AllInstalled returns a snapshot of installed item -> manifest mappings.
func (s *Store) AllInstalled() map[uint64]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneInstalled(s.installed)
}

func cloneTokens(m map[string]Token) map[string]Token { return maps.Clone(m) }

func cloneInstalled(m map[uint64]uint64) map[uint64]uint64 { return maps.Clone(m) }

// readJSON loads path into v. A missing file is not an error (v is left as-is).
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("state: read %s: %w", filepath.Base(path), err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("state: parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

// writeJSON atomically writes v as indented JSON to path.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("state: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state: replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// installed.json stores depot ids as JSON object keys (strings). encoding/json
// handles integer map keys natively, so no custom marshaling is required.
