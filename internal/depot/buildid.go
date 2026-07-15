package depot

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var buildIDRe = regexp.MustCompile(`(?i)"buildid"\s+"(\d+)"`)

// ReadInstalledBuildID finds appmanifest_233780.acf under installRoot and returns its buildid.
func ReadInstalledBuildID(installRoot string) string {
	manifestName := "appmanifest_" + DedicatedServerAppID + ".acf"
	candidates := []string{
		filepath.Join(installRoot, "steamapps", manifestName),
		filepath.Join(installRoot, manifestName),
	}

	_ = filepath.WalkDir(installRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(installRoot, path)
			if relErr == nil && strings.Count(rel, string(filepath.Separator)) > 4 {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), manifestName) {
			candidates = append(candidates, path)
		}
		return nil
	})

	seen := make(map[string]bool)
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if m := buildIDRe.FindSubmatch(data); len(m) == 2 {
			return string(m[1])
		}
	}
	return ""
}
