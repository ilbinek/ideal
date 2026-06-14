package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// AppRoot returns the directory containing the IDEAL executable.
func AppRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return mustAbs(".")
	}
	dir := filepath.Dir(exe)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// DataRoot is the directory for IDEAL runtime data (config, db, mods, branches).
// Priority: IDEAL_DATA_DIR env → directory containing config.json or arma3mods.db
// (walking up from the executable and cwd) → executable directory.
func DataRoot() string {
	if d := strings.TrimSpace(os.Getenv("IDEAL_DATA_DIR")); d != "" {
		return filepath.Clean(d)
	}
	if root := findDataRootFrom(func() (string, error) { return os.Executable() }); root != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil {
		if root := findDataRootInDir(cwd); root != "" {
			return root
		}
	}
	return AppRoot()
}

func findDataRootFrom(exeFn func() (string, error)) string {
	exe, err := exeFn()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return findDataRootWalkUp(dir)
}

func findDataRootWalkUp(start string) string {
	dir := filepath.Clean(start)
	for i := 0; i < 8; i++ {
		if findDataRootInDir(dir) != "" {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func findDataRootInDir(dir string) string {
	if hasDataMarkers(dir) {
		return dir
	}
	return ""
}

func hasDataMarkers(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "arma3mods.db")); err == nil {
		return true
	}
	return false
}

// ResolveDataPath turns a stored relative path (e.g. ./arma3/branches/main) into an
// absolute path under DataRoot. Absolute paths are returned unchanged.
func ResolveDataPath(p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	trimmed := strings.TrimPrefix(filepath.ToSlash(p), "./")
	return filepath.Join(DataRoot(), filepath.FromSlash(trimmed))
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// BranchInstallPath returns the SteamCMD force_install_dir target for a branch slug.
func BranchInstallPath(slug string) string {
	return filepath.Join(DataRoot(), "arma3", "branches", slug)
}
