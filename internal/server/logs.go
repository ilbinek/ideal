package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
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

// ListLogs returns .rpt and session .log files under the server instance.
// Inaccessible paths (e.g. root-owned profile dirs left by Docker) are skipped
// so a permission error under profiles/ cannot hide files in logs/.
func ListLogs(slug string) ([]LogEntry, error) {
	instanceDir := InstanceDir(slug)
	seen := make(map[string]struct{})
	var entries []LogEntry

	logsDir := filepath.Join(instanceDir, "logs")
	_ = collectLogFiles(logsDir, false, seen, &entries)
	_ = collectLogFiles(instanceDir, false, seen, &entries)
	profilesDir := filepath.Join(instanceDir, "profiles")
	_ = collectLogFiles(profilesDir, true, seen, &entries)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime.After(entries[j].ModTime)
	})
	return entries, nil
}

func isLogFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".rpt") || strings.HasSuffix(lower, ".log")
}

func collectLogFiles(dir string, recursive bool, seen map[string]struct{}, entries *[]LogEntry) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// Permission denied etc. — skip this tree.
		return nil
	}
	if !info.IsDir() {
		return nil
	}

	add := func(path string, fi os.FileInfo) {
		if !isLogFile(fi.Name()) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		*entries = append(*entries, LogEntry{
			Name:    fi.Name(),
			Path:    path,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		})
	}

	if !recursive {
		items, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, item := range items {
			if item.IsDir() {
				continue
			}
			fi, err := item.Info()
			if err != nil {
				continue
			}
			add(filepath.Join(dir, item.Name()), fi)
		}
		return nil
	}

	_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable dirs/files (common after Docker created root-owned paths).
			if fi != nil && fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		add(path, fi)
		return nil
	})
	return nil
}

func findLogPath(slug, filename string) (string, error) {
	if err := validateLogFilename(filename); err != nil {
		return "", err
	}
	instanceDir := InstanceDir(slug)
	candidates := []string{
		filepath.Join(instanceDir, "logs", filename),
		filepath.Join(instanceDir, filename),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	profilesDir := filepath.Join(instanceDir, "profiles")
	var found string
	_ = filepath.Walk(profilesDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info != nil && !info.IsDir() && info.Name() == filename {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("log file not found")
}

func validateLogFilename(filename string) error {
	if filename == "" || strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return fmt.Errorf("invalid filename")
	}
	return nil
}

// DeleteLog removes a log file from the server instance.
// Active session/RPT logs for a currently tracked running process cannot be deleted.
func DeleteLog(serverID int, slug, filename string) error {
	if err := validateLogFilename(filename); err != nil {
		return err
	}
	sessionName, rptName := Default.ActiveLogBasenames(serverID)
	if sessionName != "" && sessionName == filename {
		return fmt.Errorf("cannot delete the active session log while the server is running")
	}
	if rptName != "" && rptName == filename {
		return fmt.Errorf("cannot delete the active RPT while the server is running")
	}

	logPath, err := findLogPath(slug, filename)
	if err != nil {
		return err
	}
	if err := os.Remove(logPath); err != nil {
		return fmt.Errorf("failed to delete log: %w", err)
	}
	return nil
}

// LogDeleteFailure is one failed name in a bulk log delete.
type LogDeleteFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// LogDeleteResult is the outcome of DeleteLogs.
type LogDeleteResult struct {
	Deleted []string           `json:"deleted"`
	Failed  []LogDeleteFailure `json:"failed"`
}

// DeleteLogs removes multiple log files. Active-session / not-found errors go to Failed.
func DeleteLogs(serverID int, slug string, names []string) LogDeleteResult {
	res := LogDeleteResult{
		Deleted: []string{},
		Failed:  []LogDeleteFailure{},
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = filepath.Base(strings.TrimSpace(name))
		if name == "" || name == "." {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := DeleteLog(serverID, slug, name); err != nil {
			res.Failed = append(res.Failed, LogDeleteFailure{Name: name, Error: err.Error()})
			continue
		}
		res.Deleted = append(res.Deleted, name)
	}
	return res
}

// OpenLogForDownload opens a log file for streaming to the client.
func OpenLogForDownload(slug, filename string) (*os.File, string, error) {
	if err := validateLogFilename(filename); err != nil {
		return nil, "", err
	}
	logPath, err := findLogPath(slug, filename)
	if err != nil {
		return nil, "", err
	}
	f, err := openLogFile(logPath)
	if err != nil {
		return nil, "", err
	}
	return f, filepath.Base(logPath), nil
}

// ReadLog reads a log file with optional offset and limit.
// When limit is 0 a large default window is used. Content is returned from the
// given offset (from the start of the file), not only the tail, so the UI can
// show the full log and paginate with larger offsets.
// contentOffset is the byte offset of the first returned byte (after newline alignment).
// nextOffset is the file offset after the bytes consumed for this chunk (for Load more).
func ReadLog(slug, filename string, offset, limit int64) (content string, totalSize, contentOffset, nextOffset int64, err error) {
	if err := validateLogFilename(filename); err != nil {
		return "", 0, 0, 0, err
	}

	logPath, err := findLogPath(slug, filename)
	if err != nil {
		return "", 0, 0, 0, err
	}

	f, err := openLogFile(logPath)
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, 0, 0, err
	}
	totalSize = info.Size()

	const defaultLogReadLimit = 32 * 1024 * 1024 // 32 MiB
	readSize := limit
	if readSize <= 0 {
		readSize = defaultLogReadLimit
	}

	if offset < 0 {
		offset = 0
	}
	if offset > totalSize {
		offset = totalSize
	}
	contentOffset = offset

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", 0, 0, 0, err
	}

	buf := make([]byte, readSize)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", totalSize, contentOffset, contentOffset, err
	}
	buf = buf[:n]
	nextOffset = offset + int64(n)

	// If we started mid-file, drop a partial first line.
	if offset > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
			buf = buf[i+1:]
			contentOffset += int64(i + 1)
		}
	}

	return string(buf), totalSize, contentOffset, nextOffset, nil
}

// ResolveRPTPath returns the best RPT file path for tailing.
func ResolveRPTPath(slug, preferred string) string {
	if preferred != "" {
		if _, err := os.Stat(preferred); err == nil {
			return preferred
		}
	}
	path, _ := FindNewestRPT(slug)
	return path
}

// FindNewestRPT returns the most recently modified .rpt in the instance.
func FindNewestRPT(slug string) (string, error) {
	logs, err := ListLogs(slug)
	if err != nil {
		return "", err
	}
	for _, entry := range logs {
		if strings.EqualFold(filepath.Ext(entry.Name), ".rpt") {
			return entry.Path, nil
		}
	}
	return "", fmt.Errorf("no RPT log found")
}

// TailLog reads new content from a file starting at offset.
func TailLog(path string, offset int64) (string, int64, error) {
	return TailLogLimited(path, offset, 64*1024)
}

// TailLogLimited is like TailLog but caps how much is read in one call.
func TailLogLimited(path string, offset, maxRead int64) (string, int64, error) {
	f, err := openLogFile(path)
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

	if maxRead <= 0 {
		maxRead = 64 * 1024
	}
	toRead := size - offset
	if toRead > maxRead {
		toRead = maxRead
	}
	if toRead <= 0 {
		return "", size, nil
	}

	buf := make([]byte, toRead)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", offset, err
	}
	return string(buf[:n]), offset + int64(n), nil
}

// ReadLogTail returns up to maxBytes from the end of path, aligned to a newline
// when truncated. The returned size is the file size (next live-tail offset).
func ReadLogTail(path string, maxBytes int64) (content string, size int64, err error) {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	f, err := openLogFile(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	size = info.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", size, err
	}
	buf := make([]byte, size-offset)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", size, err
	}
	buf = buf[:n]
	if offset > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
			buf = buf[i+1:]
		}
	}
	return string(buf), size, nil
}

// WaitForRPT polls until a new .rpt appears or timeout.
func WaitForRPT(slug string, after time.Time, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs, err := ListLogs(slug)
		if err == nil {
			for _, entry := range logs {
				if !strings.EqualFold(filepath.Ext(entry.Name), ".rpt") {
					continue
				}
				if entry.ModTime.After(after) {
					return entry.Path, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	path, err := FindNewestRPT(slug)
	if err == nil && path != "" {
		return path, nil
	}
	return "", fmt.Errorf("timed out waiting for RPT log")
}

// portProbeHost is the address port checks bind. Empty means every interface, which
// is what production needs: Arma binds all interfaces, and a loopback-only probe can
// succeed alongside it and miss the conflict. Local test runs use loopback instead,
// because a freshly built binary binding every interface triggers a firewall prompt
// each time: always under go test, in every package, and with a loopback IDEAL_LISTEN.
var portProbeHost = defaultPortProbeHost()

func defaultPortProbeHost() string {
	if testing.Testing() {
		return "127.0.0.1"
	}
	return loopbackListenHost(os.Getenv("IDEAL_LISTEN"))
}

// loopbackListenHost returns the host of a listen address if it's loopback-only.
func loopbackListenHost(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return host
	}
	if strings.EqualFold(host, "localhost") {
		return "127.0.0.1"
	}
	return ""
}

func portProbeAddr(port int) string {
	return net.JoinHostPort(portProbeHost, strconv.Itoa(port))
}

// IsPortAvailable checks if a TCP port can be bound.
func IsPortAvailable(port int) bool {
	return isTCPPortAvailable(port) && isUDPPortAvailable(port)
}

func isTCPPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", portProbeAddr(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func isUDPPortAvailable(port int) bool {
	addr, err := net.ResolveUDPAddr("udp", portProbeAddr(port))
	if err != nil {
		return false
	}
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// IsPortInUse reports whether something is already listening on the game port
// (TCP or UDP — Arma binds UDP for gameplay).
func IsPortInUse(port int) bool {
	return !IsPortAvailable(port)
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

func validateMissionFilename(name string) error {
	if name == "" || name != filepath.Base(name) {
		return fmt.Errorf("invalid filename")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid filename")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pbo") {
		return fmt.Errorf("file must be a .pbo")
	}
	return nil
}

func missionPath(slug, name string) (string, error) {
	if err := validateMissionFilename(name); err != nil {
		return "", err
	}
	return filepath.Join(InstanceDir(slug), "mpmissions", filepath.Base(name)), nil
}

// OpenMissionForDownload opens a mission PBO for streaming to the client.
func OpenMissionForDownload(slug, name string) (*os.File, string, error) {
	path, err := missionPath(slug, name)
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("mission not found")
		}
		return nil, "", err
	}
	return f, filepath.Base(path), nil
}

// DeleteMission removes one mission PBO under mpmissions.
func DeleteMission(slug, name string) error {
	path, err := missionPath(slug, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("mission not found")
		}
		return err
	}
	return nil
}

// MissionDeleteFailure is one failed name in a bulk delete.
type MissionDeleteFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// MissionDeleteResult is the outcome of DeleteMissions.
type MissionDeleteResult struct {
	Deleted []string               `json:"deleted"`
	Failed  []MissionDeleteFailure `json:"failed"`
}

// DeleteMissions removes multiple mission PBOs. Missing files are reported in Failed.
func DeleteMissions(slug string, names []string) MissionDeleteResult {
	res := MissionDeleteResult{
		Deleted: []string{},
		Failed:  []MissionDeleteFailure{},
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = filepath.Base(strings.TrimSpace(name))
		if name == "" || name == "." {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := DeleteMission(slug, name); err != nil {
			res.Failed = append(res.Failed, MissionDeleteFailure{Name: name, Error: err.Error()})
			continue
		}
		res.Deleted = append(res.Deleted, name)
	}
	return res
}

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

// ServerKeysPreview lists keys in the server instance grouped by source.
func ServerKeysPreview(serverID int) (base, modKeys []string, custom []database.ServerKey, err error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, nil, nil, err
	}

	custom, err = database.GetServerKeys(serverID)
	if err != nil {
		return nil, nil, nil, err
	}
	if custom == nil {
		custom = []database.ServerKey{}
	}

	customSet := make(map[string]bool, len(custom))
	for _, k := range custom {
		customSet[k.Filename] = true
	}

	branchKeySet := make(map[string]bool)
	branch, err := database.GetBranch(srv.BranchID)
	if err == nil {
		contentDir, resolveErr := BranchContentDirForServer(srv.ID, branch)
		if resolveErr != nil {
			contentDir = branch.InstallPath
		}
		branchKeysDir := filepath.Join(BranchGameDir(contentDir), "keys")
		entries, readErr := os.ReadDir(branchKeysDir)
		if readErr == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if strings.HasSuffix(strings.ToLower(name), ".bikey") {
					branchKeySet[name] = true
				}
			}
		}
	}

	instanceKeysDir := filepath.Join(InstanceDir(srv.Slug), "keys")
	entries, err := os.ReadDir(instanceKeysDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, custom, nil
		}
		return nil, nil, nil, err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".bikey") || customSet[name] {
			continue
		}
		if branchKeySet[name] {
			base = append(base, name)
		} else {
			modKeys = append(modKeys, name)
		}
	}
	sort.Strings(base)
	sort.Strings(modKeys)
	return base, modKeys, custom, nil
}
