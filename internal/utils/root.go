package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// AppRoot returns the directory containing the IDEAL executable.
// Falls back to the current working directory if the executable path cannot be resolved.
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

// ResolveDataPath turns a stored relative path (e.g. ./arma3/branches/main) into an
// absolute path under AppRoot. Absolute paths are returned unchanged.
func ResolveDataPath(p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	trimmed := strings.TrimPrefix(filepath.ToSlash(p), "./")
	return filepath.Join(AppRoot(), filepath.FromSlash(trimmed))
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
