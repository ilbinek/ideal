package utils

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DedicatedServerBinaryNames lists known Arma dedicated server executable names.
var DedicatedServerBinaryNames = []string{
	"arma3server_x64",
	"arma3server_x64.exe",
	"arma3server",
	"arma3server.exe",
}

var errStopWalk = fs.ErrExist

// FindDedicatedServerBinaryRel searches installRoot for a server binary and returns
// its path relative to installRoot (forward slashes). SteamCMD installs into
// installRoot/steamapps/common/<AppName>/ rather than installRoot directly.
func FindDedicatedServerBinaryRel(installRoot string) (string, bool) {
	abs, err := filepath.Abs(installRoot)
	if err != nil {
		abs = installRoot
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false
	}

	var found string
	_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return nil
		}
		if rel != "." && strings.Count(rel, string(filepath.Separator)) > 6 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		for _, name := range DedicatedServerBinaryNames {
			if d.Name() == name {
				found = filepath.ToSlash(rel)
				return errStopWalk
			}
		}
		return nil
	})
	return found, found != ""
}

// DedicatedServerGameDir returns the directory that contains the server binary
// and game data (addons/, dta/, etc.).
func DedicatedServerGameDir(installRoot string) string {
	abs, err := filepath.Abs(installRoot)
	if err != nil {
		abs = installRoot
	}
	rel, ok := FindDedicatedServerBinaryRel(abs)
	if !ok {
		return abs
	}
	return filepath.Dir(filepath.Join(abs, filepath.FromSlash(rel)))
}

// DedicatedServerInstalled reports whether installRoot contains a server binary.
func DedicatedServerInstalled(installRoot string) bool {
	_, ok := FindDedicatedServerBinaryRel(installRoot)
	return ok
}
