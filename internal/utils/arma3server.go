package utils

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DedicatedServerBinaryNames lists preferred Arma dedicated server executable
// names in priority order (64-bit first). Used when no explicit binary is set.
var DedicatedServerBinaryNames = []string{
	"arma3server_x64",
	"arma3server_x64.exe",
	"arma3serverprofiling_x64",
	"arma3serverprofiling_x64.exe",
	"arma3server",
	"arma3server.exe",
}

// isDedicatedServerBinaryName reports whether name looks like a server executable.
func isDedicatedServerBinaryName(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "arma3server") {
		return false
	}
	switch filepath.Ext(lower) {
	case "", ".exe":
		return true
	default:
		return false
	}
}

// ListDedicatedServerBinaryRels returns relative paths (forward slashes) of all
// arma3server* executables under installRoot, sorted by preference then name.
func ListDedicatedServerBinaryRels(installRoot string) []string {
	abs, err := filepath.Abs(installRoot)
	if err != nil {
		abs = installRoot
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil
	}

	var found []string
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
		if isDedicatedServerBinaryName(d.Name()) {
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})

	sort.SliceStable(found, func(i, j int) bool {
		pi, pj := binaryPreference(filepath.Base(found[i])), binaryPreference(filepath.Base(found[j]))
		if pi != pj {
			return pi < pj
		}
		return found[i] < found[j]
	})
	return found
}

func binaryPreference(name string) int {
	for i, preferred := range DedicatedServerBinaryNames {
		if strings.EqualFold(name, preferred) {
			return i
		}
	}
	return len(DedicatedServerBinaryNames)
}

// FindDedicatedServerBinaryRel searches installRoot for a server binary and returns
// its path relative to installRoot (forward slashes). Prefers 64-bit when multiple
// are present. SteamCMD installs into installRoot/steamapps/common/<AppName>/.
func FindDedicatedServerBinaryRel(installRoot string) (string, bool) {
	found := ListDedicatedServerBinaryRels(installRoot)
	if len(found) == 0 {
		return "", false
	}
	return found[0], true
}

// ResolveDedicatedServerBinaryRel returns the relative path for preferred basename
// (e.g. "arma3server_x64"), or the auto-preferred binary when preferred is empty.
func ResolveDedicatedServerBinaryRel(installRoot, preferred string) (string, bool) {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return FindDedicatedServerBinaryRel(installRoot)
	}
	base := filepath.Base(preferred)
	for _, rel := range ListDedicatedServerBinaryRels(installRoot) {
		if strings.EqualFold(filepath.Base(rel), base) {
			return rel, true
		}
	}
	return "", false
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

// EnsureDedicatedServerBinariesExecutable sets the execute bit on arma3server*
// binaries under installRoot. Steam depot downloads often omit FlagExecutable
// (or skip rewriting unchanged files), which leaves Linux binaries non-executable.
func EnsureDedicatedServerBinariesExecutable(installRoot string) {
	for _, rel := range ListDedicatedServerBinaryRels(installRoot) {
		path := filepath.Join(installRoot, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		mode := info.Mode()
		if mode&0o111 == 0o111 {
			continue
		}
		if err := os.Chmod(path, mode|0o111); err != nil {
			// Best-effort: Start will surface permission errors if this fails.
			continue
		}
	}
}
