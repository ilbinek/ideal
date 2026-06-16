package server

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProfileFileEntry describes an editable file in a server profile directory.
type ProfileFileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

func normalizeProfileName(profileName string) string {
	name := strings.TrimSpace(profileName)
	if name == "" {
		return "Server"
	}
	return name
}

func canonicalProfileDir(slug, profileName string) string {
	name := normalizeProfileName(profileName)
	return filepath.Join(InstanceDir(slug), "profiles", "home", name)
}

func profileDirCandidates(slug, profileName string) []string {
	name := normalizeProfileName(profileName)
	profiles := filepath.Join(InstanceDir(slug), "profiles")
	return []string{
		filepath.Join(profiles, "home", name),
		filepath.Join(profiles, "Users", name),
		filepath.Join(profiles, name, "home", name),
		filepath.Join(profiles, name, "Users", name),
		filepath.Join(profiles, name),
	}
}

func dirHasProfileFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if validateProfileFilename(e.Name()) == nil {
			return true
		}
	}
	return false
}

func discoverProfileDirByWalk(slug, profileName string) string {
	name := normalizeProfileName(profileName)
	profilesRoot := filepath.Join(InstanceDir(slug), "profiles")
	target := strings.ToLower(name + ".arma3profile")

	var found string
	_ = filepath.WalkDir(profilesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(d.Name()) == target {
			found = filepath.Dir(path)
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func resolveProfileDir(slug, profileName string) string {
	for _, dir := range profileDirCandidates(slug, profileName) {
		if dirHasProfileFiles(dir) {
			return dir
		}
	}
	if dir := discoverProfileDirByWalk(slug, profileName); dir != "" {
		return dir
	}
	return canonicalProfileDir(slug, profileName)
}

func validateProfileFilename(filename string) error {
	if filename == "" ||
		strings.Contains(filename, "..") ||
		strings.Contains(filename, "/") ||
		strings.Contains(filename, "\\") {
		return fmt.Errorf("invalid profile filename")
	}
	lower := strings.ToLower(filename)
	if !strings.HasSuffix(lower, ".arma3profile") && !strings.HasSuffix(lower, ".cfg") {
		return fmt.Errorf("unsupported profile file type")
	}
	return nil
}

// ListProfileFiles returns editable files in the server's profile directory.
func ListProfileFiles(slug, profileName string) ([]ProfileFileEntry, error) {
	dir := resolveProfileDir(slug, profileName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ProfileFileEntry{}, nil
		}
		return nil, err
	}

	var files []ProfileFileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := validateProfileFilename(e.Name()); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, ProfileFileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

// ReadProfileFile reads a profile file from the server instance.
func ReadProfileFile(slug, profileName, filename string) (string, error) {
	if err := validateProfileFilename(filename); err != nil {
		return "", err
	}
	path := filepath.Join(resolveProfileDir(slug, profileName), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("profile file not found")
		}
		return "", err
	}
	return string(data), nil
}

// WriteProfileFile writes a profile file in the server instance.
func WriteProfileFile(slug, profileName, filename, content string) error {
	if err := validateProfileFilename(filename); err != nil {
		return err
	}
	dir := resolveProfileDir(slug, profileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, filename)
	return os.WriteFile(path, []byte(content), 0644)
}
