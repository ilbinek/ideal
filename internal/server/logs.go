package server

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arma3modmanager/internal/database"
)

// LogEntry describes an RPT log file.
type LogEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// ListLogs returns .rpt files under the server profiles directory.
func ListLogs(slug string) ([]LogEntry, error) {
	profilesDir := filepath.Join(InstanceDir(slug), "profiles")
	var entries []LogEntry

	err := filepath.Walk(profilesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".rpt") {
			entries = append(entries, LogEntry{
				Name:    info.Name(),
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime.After(entries[j].ModTime)
	})
	return entries, nil
}

// ReadLog reads a log file with optional offset and limit.
func ReadLog(slug, filename string, offset, limit int64) (content string, totalSize int64, err error) {
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return "", 0, fmt.Errorf("invalid filename")
	}

	profilesDir := filepath.Join(InstanceDir(slug), "profiles")
	var logPath string
	err = filepath.Walk(profilesDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && info.Name() == filename {
			logPath = path
		}
		return nil
	})
	if err != nil || logPath == "" {
		return "", 0, fmt.Errorf("log file not found")
	}

	f, err := os.Open(logPath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	totalSize = info.Size()

	if offset > totalSize {
		offset = totalSize
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", 0, err
	}

	readSize := limit
	if readSize <= 0 {
		readSize = 256 * 1024
	}
	buf := make([]byte, readSize)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", totalSize, err
	}
	return string(buf[:n]), totalSize, nil
}

// FindNewestRPT returns the most recently modified .rpt in profiles.
func FindNewestRPT(slug string) (string, error) {
	logs, err := ListLogs(slug)
	if err != nil || len(logs) == 0 {
		return "", err
	}
	return logs[0].Path, nil
}

// TailLog reads new content from a file starting at offset.
func TailLog(path string, offset int64) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", offset, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", offset, err
	}
	size := info.Size()
	if offset > size {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", offset, err
	}

	toRead := size - offset
	if toRead > 64*1024 {
		toRead = 64 * 1024
	}
	if toRead <= 0 {
		return "", size, nil
	}

	buf := make([]byte, toRead)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", offset, err
	}
	return string(buf[:n]), size, nil
}

// WaitForRPT polls until a new .rpt appears or timeout.
func WaitForRPT(slug string, after time.Time, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs, err := ListLogs(slug)
		if err == nil {
			for _, entry := range logs {
				if entry.ModTime.After(after) {
					return entry.Path, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	path, _ := FindNewestRPT(slug)
	if path != "" {
		return path, nil
	}
	return "", fmt.Errorf("timed out waiting for RPT log")
}

// IsPortAvailable checks if a TCP port can be bound.
func IsPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// ListMissions lists PBO files in mpmissions.
func ListMissions(slug string) ([]LogEntry, error) {
	missionsDir := filepath.Join(InstanceDir(slug), "mpmissions")
	var entries []LogEntry

	entriesWalk, err := os.ReadDir(missionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}

	for _, e := range entriesWalk {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".pbo") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			entries = append(entries, LogEntry{
				Name:    e.Name(),
				Path:    filepath.Join(missionsDir, e.Name()),
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// MissionUploadMaxBytes is the default max mission upload size (500 MB).
const MissionUploadMaxBytes = 500 << 20

// ConfigFileName validates and returns path for server config files.
func ConfigFilePath(slug, name string) (string, error) {
	allowed := map[string]bool{"server.cfg": true, "basic.cfg": true}
	if !allowed[name] {
		return "", fmt.Errorf("unknown config file")
	}
	return filepath.Join(InstanceDir(slug), name), nil
}

// ReadConfigFile reads a server config file.
func ReadConfigFile(slug, name string) (string, error) {
	path, err := ConfigFilePath(slug, name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("config file not found")
		}
		return "", err
	}
	return string(data), nil
}

// WriteConfigFile writes a server config file.
func WriteConfigFile(slug, name, content string) error {
	path, err := ConfigFilePath(slug, name)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// AutoKeysFromMods lists bikey filenames extracted from server mods (read-only preview).
func AutoKeysFromMods(serverID int) ([]string, error) {
	instanceDir := ""
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	instanceDir = filepath.Join(InstanceDir(srv.Slug), "keys")

	entries, err := os.ReadDir(instanceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	customKeys, _ := database.GetServerKeys(serverID)
	customSet := make(map[string]bool)
	for _, k := range customKeys {
		customSet[k.Filename] = true
	}

	var autoKeys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".bikey") && !customSet[name] {
			autoKeys = append(autoKeys, name)
		}
	}
	sort.Strings(autoKeys)
	return autoKeys, nil
}
