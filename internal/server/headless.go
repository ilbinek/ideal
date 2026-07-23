package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"arma3modmanager/internal/database"
)

type hcKey struct {
	serverID int
	slot     int
}

// HeadlessStatusSnapshot is runtime status for one HC slot.
type HeadlessStatusSnapshot struct {
	ServerID    int             `json:"serverId"`
	Slot        int             `json:"slot"`
	ProfileName string          `json:"profileName"`
	Status      string          `json:"status"`
	PID         int             `json:"pid"`
	ActiveRPT   string          `json:"activeRpt"`
	Uptime      int64           `json:"uptime"`
	StartedAt   time.Time       `json:"startedAt,omitempty"`
	Metrics     *ProcessMetrics `json:"metrics,omitempty"`
}

type runningHeadless struct {
	serverID       int
	slot           int
	gen            uint64
	pid            int
	proc           *managedProc
	startedAt      time.Time
	rptPath        string
	profileName    string
	sessionLog     *sessionLogWriter
	sessionLogPath string
	dockerMode     bool
}

func (m *Manager) ensureHCMap() {
	if m.headless == nil {
		m.headless = make(map[hcKey]*runningHeadless)
	}
}

// SetHeadlessCount persists desired count, syncs DB slots, and reconciles running processes.
func (m *Manager) SetHeadlessCount(serverID, count int) error {
	if count < 0 {
		count = 0
	}
	if count > 16 {
		return fmt.Errorf("headless count must be between 0 and 16")
	}
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}

	existing, err := database.GetHeadlessClients(serverID)
	if err != nil {
		return err
	}
	for _, hc := range existing {
		if hc.Slot > count {
			_ = m.StopHeadless(serverID, hc.Slot)
		}
	}

	clients, err := database.SyncHeadlessSlots(serverID, count, srv.Name, srv.Slug)
	if err != nil {
		return err
	}
	if err := provisionHeadlessProfiles(srv.Slug, clients); err != nil {
		return err
	}
	if count > 0 {
		_ = EnsureHeadlessClientsInCfg(srv.Slug)
	}

	// Auto-start new/missing slots when dedicated server is running.
	m.mu.Lock()
	_, serverRunning := m.instances[serverID]
	m.mu.Unlock()
	if serverRunning {
		for _, hc := range clients {
			m.mu.Lock()
			_, running := m.headless[hcKey{serverID, hc.Slot}]
			m.mu.Unlock()
			if !running {
				if err := m.StartHeadless(serverID, hc.Slot); err != nil {
					log.Printf("[HC] auto-start slot %d for server %d: %v", hc.Slot, serverID, err)
				}
			}
		}
	}

	m.broadcast()
	return nil
}

func provisionHeadlessProfiles(slug string, clients []database.HeadlessClient) error {
	instanceDir := InstanceDir(slug)
	for _, hc := range clients {
		name := strings.TrimSpace(hc.ProfileName)
		if name == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(instanceDir, "profiles", "home", name), 0755); err != nil {
			return err
		}
	}
	return nil
}

// ListHeadless returns HC snapshots for a server (DB + live state).
func (m *Manager) ListHeadless(serverID int) ([]HeadlessStatusSnapshot, error) {
	clients, err := database.GetHeadlessClients(serverID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureHCMap()

	out := make([]HeadlessStatusSnapshot, 0, len(clients))
	host := sampleHostContext()
	for _, c := range clients {
		snap := HeadlessStatusSnapshot{
			ServerID:    c.ServerID,
			Slot:        c.Slot,
			ProfileName: c.ProfileName,
			Status:      c.Status,
			PID:         c.PID,
			ActiveRPT:   c.ActiveRPT,
		}
		if inst, ok := m.headless[hcKey{serverID, c.Slot}]; ok {
			snap.PID = inst.pid
			snap.ActiveRPT = inst.rptPath
			snap.ProfileName = inst.profileName
			snap.StartedAt = inst.startedAt
			snap.Status = database.ServerStatusRunning
			if !inst.startedAt.IsZero() {
				snap.Uptime = int64(time.Since(inst.startedAt).Seconds())
			}
			snap.Metrics = sampleProcess(inst.pid)
			attachHostContext(snap.Metrics, host)
		}
		out = append(out, snap)
	}
	return out, nil
}

// StartHeadless launches one HC slot. Dedicated server should already be running.
func (m *Manager) StartHeadless(serverID, slot int) error {
	m.mu.Lock()
	m.ensureHCMap()
	key := hcKey{serverID, slot}
	if _, running := m.headless[key]; running {
		m.mu.Unlock()
		return fmt.Errorf("headless client slot %d is already running", slot)
	}
	_, serverRunning := m.instances[serverID]
	m.mu.Unlock()

	if !serverRunning {
		srv, err := database.GetServer(serverID)
		if err != nil {
			return err
		}
		if srv.Status != database.ServerStatusRunning && srv.Status != database.ServerStatusStarting {
			return fmt.Errorf("dedicated server must be running to start headless clients")
		}
	}

	hc, err := database.GetHeadlessClient(serverID, slot)
	if err != nil {
		return fmt.Errorf("headless client slot %d not found", slot)
	}
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}

	if err := EnsureHeadlessClientsInCfg(srv.Slug); err != nil {
		log.Printf("[HC] warn: ensure server.cfg HC allowlist: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(InstanceDir(srv.Slug), "profiles", "home", hc.ProfileName), 0755); err != nil {
		return err
	}

	launch, err := BuildHeadlessLaunchArgs(serverID, slot)
	if err != nil {
		return err
	}

	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStarting, 0, "")
	m.broadcast()

	startTime := time.Now()
	logsDir := filepath.Join(InstanceDir(srv.Slug), "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	sessionLogPath := filepath.Join(logsDir,
		fmt.Sprintf("hc-%d-session-%s.log", slot, startTime.Format("2006-01-02_150405")))
	sessionFile, err := os.OpenFile(sessionLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create HC session log: %w", err)
	}
	sessionLog := newSessionLogWriter(sessionFile, 512*1024)

	m.mu.Lock()
	parent, parentOK := m.instances[serverID]
	parentDocker := parentOK && parent.dockerMode
	m.mu.Unlock()
	if !parentOK {
		// Dedicated may be orphan-adopted only in DB; detect docker container.
		parentDocker = dockerContainerRunning(serverID)
	}

	var (
		proc     *managedProc
		startErr error
		pid      int
	)
	if parentDocker {
		pid, proc, startErr = execDockerHeadless(serverID, launch.Binary, launch.Args, launch.WorkingDir, sessionLogPath, sessionLog)
	} else {
		proc, startErr = startProcess(launch.Binary, launch.Args, launch.WorkingDir, sessionLog)
		if proc != nil {
			pid = proc.pid
		}
	}
	if startErr != nil {
		_ = sessionLog.Close()
		_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return startErr
	}

	if !parentDocker {
		if err := ApplyServerAffinity(proc.pid, srv.CPUAffinity); err != nil {
			log.Printf("[HC] affinity warning for server %d slot %d: %v", serverID, slot, err)
		}
		if err := ApplyServerMemoryLimit(serverID, proc.pid, srv.MemoryLimitMB); err != nil {
			log.Printf("[HC] memory limit warning for server %d slot %d: %v", serverID, slot, err)
		}
	}

	if err := verifyHeadlessStartup(proc, 5*time.Second); err != nil {
		if parentDocker {
			_ = stopDockerHeadless(serverID, pid)
		} else {
			_ = stopProcess(proc.pid)
		}
		_ = proc.wait(5 * time.Second)
		_ = sessionLog.Close()
		_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, "")
		m.broadcast()
		log.Printf("[HC] start failed for server %d slot %d: %v", serverID, slot, err)
		return err
	}

	gen := atomic.AddUint64(&nextInstanceGen, 1)
	rptPath := ResolveHeadlessRPTPath(srv.Slug, hc.ProfileName, "")

	m.mu.Lock()
	m.ensureHCMap()
	m.headless[key] = &runningHeadless{
		serverID:       serverID,
		slot:           slot,
		gen:            gen,
		pid:            pid,
		proc:           proc,
		startedAt:      startTime,
		rptPath:        rptPath,
		profileName:    hc.ProfileName,
		sessionLog:     sessionLog,
		sessionLogPath: sessionLogPath,
		dockerMode:     parentDocker,
	}
	m.mu.Unlock()

	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusRunning, pid, rptPath)
	m.broadcast()

	go m.watchHeadless(serverID, slot, gen)
	go m.resolveHeadlessRPT(serverID, slot, gen, hc.ProfileName, startTime)
	return nil
}

func verifyHeadlessStartup(proc *managedProc, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if proc.exited() {
			return fmt.Errorf("headless client process exited immediately after start (check session log)")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if proc.exited() {
		return fmt.Errorf("headless client process exited immediately after start (check session log)")
	}
	return nil
}

func (m *Manager) resolveHeadlessRPT(serverID, slot int, gen uint64, profileName string, startTime time.Time) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return
	}
	rptPath, _ := WaitForHeadlessRPT(srv.Slug, profileName, startTime.Add(-time.Second), 30*time.Second)
	if rptPath == "" {
		return
	}
	m.mu.Lock()
	inst, ok := m.headless[hcKey{serverID, slot}]
	if !ok || inst.gen != gen {
		m.mu.Unlock()
		return
	}
	inst.rptPath = rptPath
	pid := inst.pid
	m.mu.Unlock()
	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusRunning, pid, rptPath)
	m.broadcast()
}

// StopHeadless stops one HC slot.
func (m *Manager) StopHeadless(serverID, slot int) error {
	m.mu.Lock()
	m.ensureHCMap()
	key := hcKey{serverID, slot}
	inst, ok := m.headless[key]
	m.mu.Unlock()

	if !ok {
		return m.stopHeadlessOrphan(serverID, slot)
	}

	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopping, inst.pid, inst.rptPath)
	m.broadcast()

	var stopErr error
	if inst.dockerMode {
		stopErr = stopDockerHeadless(serverID, inst.pid)
	} else {
		stopErr = stopProcess(inst.pid)
	}
	if stopErr != nil {
		stillAlive := false
		if inst.dockerMode {
			stillAlive = !inst.proc.exited() && (processRunning(inst.pid) || dockerExecStillAlive(serverID, inst.pid))
		} else {
			stillAlive = !inst.proc.exited() && processRunning(inst.pid)
		}
		if stillAlive {
			_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusRunning, inst.pid, inst.rptPath)
			m.broadcast()
			return stopErr
		}
	}

	_ = inst.proc.wait(30 * time.Second)

	m.mu.Lock()
	if cur, ok := m.headless[key]; ok && cur.gen == inst.gen {
		m.closeHeadlessLog(cur)
		delete(m.headless, key)
	}
	m.mu.Unlock()

	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, inst.rptPath)
	m.broadcast()
	return nil
}

func (m *Manager) stopHeadlessOrphan(serverID, slot int) error {
	hc, err := database.GetHeadlessClient(serverID, slot)
	if err != nil {
		return err
	}
	if hc.PID > 0 {
		if dockerContainerRunning(serverID) {
			_ = stopDockerHeadless(serverID, hc.PID)
		} else if processRunning(hc.PID) {
			if err := stopProcess(hc.PID); err != nil && processRunning(hc.PID) {
				return err
			}
		}
	}
	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, hc.ActiveRPT)
	m.broadcast()
	return nil
}

func (m *Manager) closeHeadlessLog(inst *runningHeadless) {
	if inst == nil || inst.sessionLog == nil {
		return
	}
	_ = inst.sessionLog.Close()
	inst.sessionLog = nil
}

// RestartHeadless stops then starts one HC slot.
func (m *Manager) RestartHeadless(serverID, slot int) error {
	if err := m.StopHeadless(serverID, slot); err != nil {
		return err
	}
	return m.StartHeadless(serverID, slot)
}

// StopAllHeadless stops every HC for a server.
func (m *Manager) StopAllHeadless(serverID int) {
	clients, err := database.GetHeadlessClients(serverID)
	if err != nil {
		return
	}
	for _, hc := range clients {
		if err := m.StopHeadless(serverID, hc.Slot); err != nil {
			log.Printf("[HC] stop slot %d for server %d: %v", hc.Slot, serverID, err)
		}
	}
}

// StartAllHeadless starts every configured HC for a server.
func (m *Manager) StartAllHeadless(serverID int) {
	clients, err := database.GetHeadlessClients(serverID)
	if err != nil {
		return
	}
	for _, hc := range clients {
		if err := m.StartHeadless(serverID, hc.Slot); err != nil {
			log.Printf("[HC] start slot %d for server %d: %v", hc.Slot, serverID, err)
		}
	}
}

// ActiveHeadlessRPTPath returns the active RPT for an HC slot.
func (m *Manager) ActiveHeadlessRPTPath(serverID, slot int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.headless[hcKey{serverID, slot}]; ok {
		return inst.rptPath
	}
	hc, err := database.GetHeadlessClient(serverID, slot)
	if err == nil {
		return hc.ActiveRPT
	}
	return ""
}

// ActiveHeadlessSessionLogPath returns the session log for a running HC.
func (m *Manager) ActiveHeadlessSessionLogPath(serverID, slot int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.headless[hcKey{serverID, slot}]; ok {
		return inst.sessionLogPath
	}
	return ""
}

// ActiveHeadlessLogBasenames returns session/RPT filenames for a tracked HC process.
func (m *Manager) ActiveHeadlessLogBasenames(serverID, slot int) (session, rpt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.headless[hcKey{serverID, slot}]
	if !ok {
		return "", ""
	}
	if inst.sessionLogPath != "" {
		session = filepath.Base(inst.sessionLogPath)
	}
	if inst.rptPath != "" {
		rpt = filepath.Base(inst.rptPath)
	}
	return session, rpt
}

// IsHeadlessRunning reports whether Ideal is tracking a live HC process.
func (m *Manager) IsHeadlessRunning(serverID, slot int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.headless[hcKey{serverID, slot}]
	return ok
}

func (m *Manager) watchHeadless(serverID, slot int, gen uint64) {
	const pollInterval = 2 * time.Second
	const deadThreshold = 2

	key := hcKey{serverID, slot}
	consecutiveDead := 0
	for {
		time.Sleep(pollInterval)

		m.mu.Lock()
		inst, tracked := m.headless[key]
		sameGen := tracked && inst.gen == gen
		var proc *managedProc
		var pid int
		if sameGen {
			proc = inst.proc
			pid = inst.pid
		}
		m.mu.Unlock()
		if !tracked || !sameGen {
			return
		}

		if proc != nil && proc.exited() {
			// exited
		} else if processRunning(pid) {
			consecutiveDead = 0
			m.broadcast()
			continue
		}

		consecutiveDead++
		if consecutiveDead < deadThreshold {
			continue
		}
		break
	}

	m.mu.Lock()
	inst, ok := m.headless[key]
	if !ok || inst.gen != gen {
		m.mu.Unlock()
		return
	}
	if inst.proc != nil {
		_ = inst.proc.wait(30 * time.Second)
	}
	rpt := inst.rptPath
	m.closeHeadlessLog(inst)
	delete(m.headless, key)
	m.mu.Unlock()

	_ = database.UpdateHeadlessStatus(serverID, slot, database.ServerStatusStopped, 0, rpt)
	m.broadcast()
}

// ReconcileHeadlessStatuses settles orphaned HC DB statuses on Ideal startup.
func ReconcileHeadlessStatuses() {
	clients, err := database.GetAllHeadlessClients()
	if err != nil {
		log.Printf("[HC] failed to reconcile statuses: %v", err)
		return
	}
	for _, c := range clients {
		alive := c.PID > 0 && processRunning(c.PID)
		switch c.Status {
		case database.ServerStatusRunning, database.ServerStatusStarting:
			if !alive {
				_ = database.UpdateHeadlessStatus(c.ServerID, c.Slot, database.ServerStatusUnknown, 0, c.ActiveRPT)
			}
		case database.ServerStatusStopping:
			if alive {
				_ = stopProcess(c.PID)
			}
			_ = database.UpdateHeadlessStatus(c.ServerID, c.Slot, database.ServerStatusStopped, 0, c.ActiveRPT)
		case database.ServerStatusUnknown:
			if !alive {
				_ = database.UpdateHeadlessStatus(c.ServerID, c.Slot, database.ServerStatusStopped, 0, c.ActiveRPT)
			}
		}
	}
}

var (
	headlessClientsArrayRe = regexp.MustCompile(`(?is)headlessClients\s*\[\s*\]\s*=\s*\{([^}]*)\}`)
	localClientArrayRe     = regexp.MustCompile(`(?is)localClient\s*\[\s*\]\s*=\s*\{([^}]*)\}`)
)

// EnsureHeadlessClientsInCfg ensures 127.0.0.1 is listed in headlessClients and localClient.
func EnsureHeadlessClientsInCfg(slug string) error {
	path := filepath.Join(InstanceDir(slug), "server.cfg")
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	changed := false

	ensureArray := func(re *regexp.Regexp, key string) {
		if re.MatchString(text) {
			text = re.ReplaceAllStringFunc(text, func(match string) string {
				sub := re.FindStringSubmatch(match)
				inner := ""
				if len(sub) > 1 {
					inner = sub[1]
				}
				if strings.Contains(inner, "127.0.0.1") {
					return match
				}
				inner = strings.TrimSpace(inner)
				if inner == "" {
					inner = `"127.0.0.1"`
				} else {
					inner = strings.TrimRight(inner, " \t\r\n,") + `, "127.0.0.1"`
				}
				changed = true
				return key + "[] = {" + inner + "}"
			})
			return
		}
		changed = true
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += key + `[] = {"127.0.0.1"};` + "\n"
	}

	ensureArray(headlessClientsArrayRe, "headlessClients")
	ensureArray(localClientArrayRe, "localClient")

	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(text), 0644)
}

// ListHeadlessLogs returns logs belonging to one HC (profile RPTs + hc-N session logs).
func ListHeadlessLogs(slug string, slot int, profileName string) ([]LogEntry, error) {
	all, err := ListLogs(slug)
	if err != nil {
		return nil, err
	}
	sessionPrefix := fmt.Sprintf("hc-%d-session-", slot)
	sep := string(filepath.Separator)
	var out []LogEntry
	for _, e := range all {
		if slot > 0 && strings.HasPrefix(e.Name, sessionPrefix) {
			out = append(out, e)
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name), ".rpt") || profileName == "" {
			continue
		}
		if strings.Contains(e.Path, sep+profileName+sep) {
			out = append(out, e)
		}
	}
	return out, nil
}

// DeleteHeadlessLog removes a log that belongs to the given HC slot.
func DeleteHeadlessLog(serverID, slot int, slug, profileName, filename string) error {
	if err := validateLogFilename(filename); err != nil {
		return err
	}
	sessionName, rptName := Default.ActiveHeadlessLogBasenames(serverID, slot)
	if sessionName != "" && sessionName == filename {
		return fmt.Errorf("cannot delete the active session log while the headless client is running")
	}
	if rptName != "" && rptName == filename {
		return fmt.Errorf("cannot delete the active RPT while the headless client is running")
	}

	logs, err := ListHeadlessLogs(slug, slot, profileName)
	if err != nil {
		return err
	}
	var path string
	for _, e := range logs {
		if e.Name == filename {
			path = e.Path
			break
		}
	}
	if path == "" {
		return fmt.Errorf("log file not found")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete log: %w", err)
	}
	return nil
}

// OpenHeadlessLogForDownload opens an HC-owned log for download.
func OpenHeadlessLogForDownload(slug string, slot int, profileName, filename string) (*os.File, string, error) {
	if err := validateLogFilename(filename); err != nil {
		return nil, "", err
	}
	logs, err := ListHeadlessLogs(slug, slot, profileName)
	if err != nil {
		return nil, "", err
	}
	var path string
	for _, e := range logs {
		if e.Name == filename {
			path = e.Path
			break
		}
	}
	if path == "" {
		return nil, "", fmt.Errorf("log file not found")
	}
	f, err := openLogFile(path)
	if err != nil {
		return nil, "", err
	}
	return f, filepath.Base(path), nil
}

// ResolveHeadlessRPTPath returns the best RPT for an HC profile.
func ResolveHeadlessRPTPath(slug, profileName, preferred string) string {
	if preferred != "" {
		if _, err := os.Stat(preferred); err == nil {
			return preferred
		}
	}
	path, _ := FindNewestHeadlessRPT(slug, profileName)
	return path
}

// FindNewestHeadlessRPT returns the newest .rpt under the HC profile directory.
func FindNewestHeadlessRPT(slug, profileName string) (string, error) {
	logs, err := ListHeadlessLogs(slug, 0, profileName)
	if err != nil || len(logs) == 0 {
		return "", err
	}
	for _, e := range logs {
		if strings.EqualFold(filepath.Ext(e.Name), ".rpt") {
			return e.Path, nil
		}
	}
	return "", nil
}

// WaitForHeadlessRPT polls until a new HC .rpt appears or timeout.
func WaitForHeadlessRPT(slug, profileName string, after time.Time, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs, err := ListHeadlessLogs(slug, 0, profileName)
		if err == nil {
			for _, e := range logs {
				if !strings.EqualFold(filepath.Ext(e.Name), ".rpt") {
					continue
				}
				if e.ModTime.After(after) {
					return e.Path, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return FindNewestHeadlessRPT(slug, profileName)
}
