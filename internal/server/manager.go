package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
)

// StatusSnapshot is the runtime status of one server.
type StatusSnapshot struct {
	ID        int                      `json:"id"`
	Name      string                   `json:"name"`
	Slug      string                   `json:"slug"`
	Status    string                   `json:"status"`
	PID       int                      `json:"pid"`
	ActiveRPT string                   `json:"activeRpt"`
	Uptime    int64                    `json:"uptime"` // seconds, 0 if not running
	StartedAt time.Time                `json:"startedAt,omitempty"`
	Metrics   *ProcessMetrics          `json:"metrics,omitempty"`
	Headless  []HeadlessStatusSnapshot `json:"headless,omitempty"`
}

// AllStatusSnapshot is broadcast to SSE subscribers.
type AllStatusSnapshot struct {
	Servers []StatusSnapshot `json:"servers"`
}

// Manager controls running Arma server processes.
type Manager struct {
	mu        sync.Mutex
	instances map[int]*runningInstance
	headless  map[hcKey]*runningHeadless

	subMu sync.Mutex
	subs  map[chan AllStatusSnapshot]struct{}
}

type runningInstance struct {
	serverID       int
	gen            uint64
	pid            int
	proc           *managedProc
	startedAt      time.Time
	rptPath        string
	sessionLog     *sessionLogWriter
	sessionLogPath string
	dockerMode     bool
	containerID    string
}

// Default is the process-wide server manager.
var Default = &Manager{
	instances: make(map[int]*runningInstance),
	headless:  make(map[hcKey]*runningHeadless),
	subs:      make(map[chan AllStatusSnapshot]struct{}),
}

var nextInstanceGen uint64

// Init reconciles DB state on startup and re-adopts still-running processes
// so metrics, watchers, and Stop keep working across Ideal restarts.
func Init() {
	ReconcileStatuses()
	ReconcileHeadlessStatuses()
	Default.adoptOrphans()
	// Drop unused old generations after adopt so pins for live servers are kept.
	contentver.GCAllOrphans()
}

// ReconcileStatuses settles orphaned DB statuses on startup. Servers still bound
// to their game port are left alone when marked running/starting (Arma often
// outlives the launcher PID we captured). Transitional "stopping" is always
// recovered so a mid-stop reboot cannot trap the UI.
func ReconcileStatuses() {
	servers, err := database.GetServers()
	if err != nil {
		log.Printf("[SERVER] failed to reconcile statuses: %v", err)
		return
	}
	for _, s := range servers {
		alive := ServerAlive(s.PID, s.GamePort) || dockerContainerRunning(s.ID)
		switch s.Status {
		case database.ServerStatusRunning, database.ServerStatusStarting:
			if !alive {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, 0, s.ActiveRPT)
				if pins, err := database.ClearServerPins(s.ID); err == nil {
					contentver.GCReleasedPins(pins)
				}
			}
		case database.ServerStatusStopping:
			if alive && dockerContainerRunning(s.ID) {
				_ = stopDockerContainer(s.ID)
			} else if alive && s.PID > 0 && processRunning(s.PID) {
				if err := stopProcess(s.PID); err != nil && ServerAlive(s.PID, s.GamePort) {
					// Kill failed and process still up — unknown so Start/Stop stay usable.
					_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, s.PID, s.ActiveRPT)
					continue
				}
			}
			if ServerAlive(s.PID, s.GamePort) || dockerContainerRunning(s.ID) {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, s.PID, s.ActiveRPT)
				continue
			}
			_ = database.UpdateServerStatus(s.ID, database.ServerStatusStopped, 0, s.ActiveRPT)
			if pins, err := database.ClearServerPins(s.ID); err == nil {
				contentver.GCReleasedPins(pins)
			}
		case database.ServerStatusUnknown:
			if !alive {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusStopped, 0, s.ActiveRPT)
				if pins, err := database.ClearServerPins(s.ID); err == nil {
					contentver.GCReleasedPins(pins)
				}
			}
		}
	}
}

// adoptOrphans re-attaches Ideal to Arma / headless PIDs that survived a restart.
// Without this, DB status stays "running" but metrics and watchers never resume.
func (m *Manager) adoptOrphans() {
	servers, err := database.GetServers()
	if err != nil {
		log.Printf("[SERVER] failed to adopt orphans: %v", err)
		return
	}

	m.mu.Lock()
	m.ensureHCMap()
	var adoptedServers []struct {
		id  int
		gen uint64
	}
	for _, s := range servers {
		if _, ok := m.instances[s.ID]; ok {
			continue
		}
		if s.Status != database.ServerStatusRunning && s.Status != database.ServerStatusStarting {
			continue
		}

		var (
			pid         int
			proc        *managedProc
			dockerMode  bool
			containerID string
		)

		// Prefer Docker containers when present (Ideal may have restarted with useDocker).
		if cid, dproc, dpid, derr := adoptDockerContainer(s.ID); derr == nil && dproc != nil {
			pid = dpid
			proc = dproc
			dockerMode = true
			containerID = cid
		} else if s.PID > 0 && processRunning(s.PID) {
			pid = s.PID
			proc = adoptProcess(s.PID)
		} else {
			continue
		}

		gen := atomic.AddUint64(&nextInstanceGen, 1)
		m.instances[s.ID] = &runningInstance{
			serverID:    s.ID,
			gen:         gen,
			pid:         pid,
			proc:        proc,
			startedAt:   time.Now(),
			rptPath:     s.ActiveRPT,
			dockerMode:  dockerMode,
			containerID: containerID,
		}
		adoptedServers = append(adoptedServers, struct {
			id  int
			gen uint64
		}{s.ID, gen})
		if s.Status != database.ServerStatusRunning {
			_ = database.UpdateServerStatus(s.ID, database.ServerStatusRunning, pid, s.ActiveRPT)
		}
		if !dockerMode {
			if err := ApplyServerAffinity(pid, s.CPUAffinity); err != nil {
				log.Printf("[SERVER] affinity on adopt pid=%d server=%d: %v", pid, s.ID, err)
			}
			if err := ApplyServerMemoryLimit(s.ID, pid, s.MemoryLimitMB); err != nil {
				log.Printf("[SERVER] memory limit on adopt pid=%d server=%d: %v", pid, s.ID, err)
			}
		}
		if dockerMode {
			log.Printf("[SERVER] adopted docker orphan container=%s pid=%d server=%d (%s)", containerID, pid, s.ID, s.Slug)
		} else {
			log.Printf("[SERVER] adopted orphan pid=%d server=%d (%s)", pid, s.ID, s.Slug)
		}
	}

	clients, _ := database.GetAllHeadlessClients()
	var adoptedHC []struct {
		serverID int
		slot     int
		gen      uint64
	}
	for _, c := range clients {
		key := hcKey{c.ServerID, c.Slot}
		if _, ok := m.headless[key]; ok {
			continue
		}
		if c.Status != database.ServerStatusRunning && c.Status != database.ServerStatusStarting {
			continue
		}
		if c.PID <= 0 || !processRunning(c.PID) {
			continue
		}
		parentDocker := false
		if inst, ok := m.instances[c.ServerID]; ok {
			parentDocker = inst.dockerMode
		} else if dockerContainerRunning(c.ServerID) {
			parentDocker = true
		}
		gen := atomic.AddUint64(&nextInstanceGen, 1)
		m.headless[key] = &runningHeadless{
			serverID:    c.ServerID,
			slot:        c.Slot,
			gen:         gen,
			pid:         c.PID,
			proc:        adoptProcess(c.PID),
			startedAt:   time.Now(),
			rptPath:     c.ActiveRPT,
			profileName: c.ProfileName,
			dockerMode:  parentDocker,
		}
		adoptedHC = append(adoptedHC, struct {
			serverID int
			slot     int
			gen      uint64
		}{c.ServerID, c.Slot, gen})
		if c.Status != database.ServerStatusRunning {
			_ = database.UpdateHeadlessStatus(c.ServerID, c.Slot, database.ServerStatusRunning, c.PID, c.ActiveRPT)
		}
		if !parentDocker {
			if parent, err := database.GetServer(c.ServerID); err == nil {
				if err := ApplyServerAffinity(c.PID, parent.CPUAffinity); err != nil {
					log.Printf("[HC] affinity on adopt pid=%d server=%d slot=%d: %v", c.PID, c.ServerID, c.Slot, err)
				}
				if err := ApplyServerMemoryLimit(c.ServerID, c.PID, parent.MemoryLimitMB); err != nil {
					log.Printf("[HC] memory limit on adopt pid=%d server=%d slot=%d: %v", c.PID, c.ServerID, c.Slot, err)
				}
			}
		}
		log.Printf("[HC] adopted orphan pid=%d server=%d slot=%d", c.PID, c.ServerID, c.Slot)
	}
	m.mu.Unlock()

	for _, a := range adoptedServers {
		go m.watchServer(a.id, a.gen)
	}
	for _, a := range adoptedHC {
		go m.watchHeadless(a.serverID, a.slot, a.gen)
	}
	if len(adoptedServers) > 0 || len(adoptedHC) > 0 {
		m.broadcast()
	}
}

// Snapshot returns current status for all servers.
func (m *Manager) Snapshot() AllStatusSnapshot {
	servers, err := database.GetServers()
	if err != nil {
		return AllStatusSnapshot{Servers: []StatusSnapshot{}}
	}

	allHC, _ := database.GetAllHeadlessClients()
	hcByServer := make(map[int][]database.HeadlessClient)
	for _, c := range allHC {
		hcByServer[c.ServerID] = append(hcByServer[c.ServerID], c)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureHCMap()

	var snaps []StatusSnapshot
	host := sampleHostContext()
	for _, s := range servers {
		snap := StatusSnapshot{
			ID:        s.ID,
			Name:      s.Name,
			Slug:      s.Slug,
			Status:    s.Status,
			PID:       s.PID,
			ActiveRPT: s.ActiveRPT,
		}
		if inst, ok := m.instances[s.ID]; ok {
			snap.PID = inst.pid
			snap.ActiveRPT = inst.rptPath
			snap.StartedAt = inst.startedAt
			if !inst.startedAt.IsZero() {
				snap.Uptime = int64(time.Since(inst.startedAt).Seconds())
			}
			snap.Metrics = sampleProcess(inst.pid)
			attachHostContext(snap.Metrics, host)
		} else if (s.Status == database.ServerStatusRunning || s.Status == database.ServerStatusStarting) &&
			s.PID > 0 && processRunning(s.PID) {
			// DB says running but not yet tracked (or adopt missed) — still expose metrics.
			snap.Metrics = sampleProcess(s.PID)
			attachHostContext(snap.Metrics, host)
		}
		for _, c := range hcByServer[s.ID] {
			hcSnap := HeadlessStatusSnapshot{
				ServerID:    c.ServerID,
				Slot:        c.Slot,
				ProfileName: c.ProfileName,
				Status:      c.Status,
				PID:         c.PID,
				ActiveRPT:   c.ActiveRPT,
			}
			if inst, ok := m.headless[hcKey{s.ID, c.Slot}]; ok {
				hcSnap.PID = inst.pid
				hcSnap.ActiveRPT = inst.rptPath
				hcSnap.ProfileName = inst.profileName
				hcSnap.StartedAt = inst.startedAt
				hcSnap.Status = database.ServerStatusRunning
				if !inst.startedAt.IsZero() {
					hcSnap.Uptime = int64(time.Since(inst.startedAt).Seconds())
				}
				hcSnap.Metrics = sampleProcess(inst.pid)
				attachHostContext(hcSnap.Metrics, host)
			} else if (c.Status == database.ServerStatusRunning || c.Status == database.ServerStatusStarting) &&
				c.PID > 0 && processRunning(c.PID) {
				hcSnap.Metrics = sampleProcess(c.PID)
				attachHostContext(hcSnap.Metrics, host)
			}
			snap.Headless = append(snap.Headless, hcSnap)
		}
		snaps = append(snaps, snap)
	}
	pruneMetricsCache()
	return AllStatusSnapshot{Servers: snaps}
}

// Subscribe registers an SSE subscriber.
func (m *Manager) Subscribe() chan AllStatusSnapshot {
	ch := make(chan AllStatusSnapshot, 4)
	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	ch <- m.Snapshot()
	return ch
}

// Unsubscribe removes a subscriber.
func (m *Manager) Unsubscribe(ch chan AllStatusSnapshot) {
	m.subMu.Lock()
	if _, ok := m.subs[ch]; ok {
		delete(m.subs, ch)
		close(ch)
	}
	m.subMu.Unlock()
}

func (m *Manager) broadcast() {
	snap := m.Snapshot()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}

// Start provisions and launches a server.
func (m *Manager) Start(serverID int) error {
	m.mu.Lock()
	if _, running := m.instances[serverID]; running {
		m.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	m.mu.Unlock()

	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return err
	}
	if !BranchIsInstalled(branch) {
		return fmt.Errorf("branch %q is not installed", branch.Name)
	}

	conflicts, err := database.GetServersUsingPort(srv.GamePort, srv.SteamPort, serverID)
	if err != nil {
		return err
	}
	for _, c := range conflicts {
		if c.Status == database.ServerStatusRunning || c.Status == database.ServerStatusStarting {
			return fmt.Errorf("port conflict with server %q", c.Name)
		}
	}
	if err := waitForPortsFree(srv.GamePort, srv.SteamPort, 30*time.Second); err != nil {
		return err
	}

	if err := FindRunningAffinityConflict(serverID, ParseAffinity(srv.CPUAffinity)); err != nil {
		return err
	}

	pins, err := contentver.CollectPinsForServer(serverID)
	if err != nil {
		return fmt.Errorf("failed to collect content pins: %w", err)
	}
	if err := database.ReplaceServerPins(serverID, pins); err != nil {
		return fmt.Errorf("failed to pin content versions: %w", err)
	}

	if err := ProvisionServer(serverID); err != nil {
		_, _ = database.ClearServerPins(serverID)
		return err
	}

	launch, err := BuildLaunchArgs(serverID)
	if err != nil {
		_, _ = database.ClearServerPins(serverID)
		return err
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStarting, 0, "")
	m.broadcast()

	startTime := time.Now()
	if err := os.MkdirAll(filepath.Join(InstanceDir(srv.Slug), "logs"), 0755); err != nil {
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	sessionLogPath := filepath.Join(InstanceDir(srv.Slug), "logs",
		fmt.Sprintf("session-%s.log", startTime.Format("2006-01-02_150405")))
	sessionFile, err := os.OpenFile(sessionLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create session log: %w", err)
	}
	sessionLog := newSessionLogWriter(sessionFile, 512*1024)

	useDocker, dockerErr := dockerModeForStart()
	if dockerErr != nil {
		_ = sessionLog.Close()
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return dockerErr
	}
	var (
		proc        *managedProc
		containerID string
		startErr    error
	)
	if useDocker {
		extraMounts := CollectDockerExtraMounts(serverID)
		containerID, proc, startErr = startDockerServer(
			serverID, launch.Binary, launch.Args, launch.WorkingDir,
			srv.CPUAffinity, srv.MemoryLimitMB, extraMounts, sessionLog,
		)
	} else {
		proc, startErr = startProcess(launch.Binary, launch.Args, launch.WorkingDir, sessionLog)
	}
	if startErr != nil {
		_ = sessionLog.Close()
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return startErr
	}

	if !useDocker {
		if err := ApplyServerAffinity(proc.pid, srv.CPUAffinity); err != nil {
			log.Printf("[SERVER] affinity warning for %d: %v", serverID, err)
		}
		if err := ApplyServerMemoryLimit(serverID, proc.pid, srv.MemoryLimitMB); err != nil {
			log.Printf("[SERVER] memory limit warning for %d: %v", serverID, err)
		}
	}

	// Confirm the process did not die immediately (e.g. port still held on UDP).
	if err := verifyProcessStartup(proc, srv.GamePort, 8*time.Second); err != nil {
		if useDocker {
			_ = stopDockerContainer(serverID)
		} else {
			_ = stopProcess(proc.pid)
		}
		_ = proc.wait(5 * time.Second)
		_ = sessionLog.Close()
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		log.Printf("[SERVER] start failed for %d: %v", serverID, err)
		return err
	}

	gen := atomic.AddUint64(&nextInstanceGen, 1)
	rptPath := ResolveRPTPath(srv.Slug, "")

	m.mu.Lock()
	m.instances[serverID] = &runningInstance{
		serverID:       serverID,
		gen:            gen,
		pid:            proc.pid,
		proc:           proc,
		startedAt:      startTime,
		rptPath:        rptPath,
		sessionLog:     sessionLog,
		sessionLogPath: sessionLogPath,
		dockerMode:     useDocker,
		containerID:    containerID,
	}
	m.mu.Unlock()

	_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, proc.pid, rptPath)
	m.broadcast()

	go m.watchServer(serverID, gen)
	go m.resolveRPT(serverID, gen, startTime)

	// Start configured headless clients after the dedicated server is up.
	go func() {
		time.Sleep(2 * time.Second)
		m.StartAllHeadless(serverID)
	}()
	return nil
}

func waitForPortsFree(gamePort, steamPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		gameFree := IsPortAvailable(gamePort)
		steamFree := IsPortAvailable(steamPort)
		if gameFree && steamFree {
			return nil
		}
		if time.Now().After(deadline) {
			if !gameFree {
				return fmt.Errorf("game port %d is not available", gamePort)
			}
			return fmt.Errorf("steam port %d is not available", steamPort)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// verifyProcessStartup ensures the child did not exit right away. Prefer seeing
// the game port bind; if Arma is still starting after timeout, allow it.
func verifyProcessStartup(proc *managedProc, gamePort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	sawAlive := false
	for time.Now().Before(deadline) {
		if !proc.exited() {
			sawAlive = true
			if IsPortInUse(gamePort) {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		// Process exited. If something else (or a child) holds the port, treat as OK.
		if IsPortInUse(gamePort) {
			return nil
		}
		if sawAlive {
			return fmt.Errorf("server process exited immediately after start (check session log / ports)")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if proc.exited() && !IsPortInUse(gamePort) {
		return fmt.Errorf("server process exited immediately after start (check session log / ports)")
	}
	return nil
}

func (m *Manager) resolveRPT(serverID int, gen uint64, startTime time.Time) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return
	}
	rptPath, _ := WaitForRPT(srv.Slug, startTime.Add(-time.Second), 30*time.Second)
	if rptPath == "" {
		return
	}
	m.mu.Lock()
	inst, ok := m.instances[serverID]
	if !ok || inst.gen != gen {
		m.mu.Unlock()
		return
	}
	inst.rptPath = rptPath
	pid := inst.pid
	m.mu.Unlock()
	_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, pid, rptPath)
	m.broadcast()
}

// Stop stops a running server.
func (m *Manager) Stop(serverID int) error {
	m.StopAllHeadless(serverID)

	m.mu.Lock()
	inst, ok := m.instances[serverID]
	m.mu.Unlock()

	if !ok {
		return m.stopOrphan(serverID)
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopping, inst.pid, inst.rptPath)
	m.broadcast()

	srv, _ := database.GetServer(serverID)
	gamePort, steamPort := 0, 0
	if srv != nil {
		gamePort = srv.GamePort
		steamPort = srv.SteamPort
	}

	var stopErr error
	if inst.dockerMode {
		stopErr = stopDockerContainer(serverID)
	} else {
		stopErr = stopProcess(inst.pid)
	}
	if stopErr != nil {
		stillAlive := false
		if inst.dockerMode {
			stillAlive = dockerContainerRunning(serverID)
		} else {
			stillAlive = !inst.proc.exited() && ServerAlive(inst.pid, gamePort)
		}
		if stillAlive {
			// Still running — revert so status cannot stay stuck on "stopping".
			_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, inst.pid, inst.rptPath)
			m.broadcast()
			return stopErr
		}
		// Process already gone; treat as a successful stop.
	}

	// Wait until stdout/stderr pipes are fully drained before closing the log.
	_ = inst.proc.wait(30 * time.Second)

	m.mu.Lock()
	// Only clear if this is still the instance we stopped (Restart race safety).
	if cur, ok := m.instances[serverID]; ok && cur.gen == inst.gen {
		m.closeInstanceLog(cur)
		delete(m.instances, serverID)
	}
	m.mu.Unlock()

	if gamePort > 0 {
		_ = waitForPortsFree(gamePort, steamPort, 30*time.Second)
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, inst.rptPath)
	ReleaseServerMemoryLimit(serverID)
	if pins, err := database.ClearServerPins(serverID); err == nil {
		contentver.GCReleasedPins(pins)
	}
	m.broadcast()
	return nil
}

// stopOrphan clears status for a server not tracked in-memory (e.g. after restart).
func (m *Manager) stopOrphan(serverID int) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	if _, _, running, _ := findDockerContainer(serverID); running || dockerContainerRunning(serverID) {
		_ = stopDockerContainer(serverID)
	} else if srv.PID > 0 && processRunning(srv.PID) {
		if err := stopProcess(srv.PID); err != nil && ServerAlive(srv.PID, srv.GamePort) {
			return err
		}
	}
	_ = waitForPortsFree(srv.GamePort, srv.SteamPort, 15*time.Second)
	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, srv.ActiveRPT)
	ReleaseServerMemoryLimit(serverID)
	if pins, err := database.ClearServerPins(serverID); err == nil {
		contentver.GCReleasedPins(pins)
	}
	m.broadcast()
	return nil
}

func (m *Manager) closeInstanceLog(inst *runningInstance) {
	if inst == nil || inst.sessionLog == nil {
		return
	}
	_ = inst.sessionLog.Close()
	inst.sessionLog = nil
}

// Restart stops then starts a server.
func (m *Manager) Restart(serverID int) error {
	if err := m.Stop(serverID); err != nil {
		return err
	}
	return m.Start(serverID)
}

// ActiveRPTPath returns the active RPT path for a running server.
// Falls back to the last known DB path (useful for log streaming after Ideal restart).
func (m *Manager) ActiveRPTPath(serverID int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[serverID]; ok {
		return inst.rptPath
	}
	srv, err := database.GetServer(serverID)
	if err == nil {
		return srv.ActiveRPT
	}
	return ""
}

// ActiveLogBasenames returns session/RPT filenames only when Ideal is currently
// tracking a live process for this server. Empty when stopped.
func (m *Manager) ActiveLogBasenames(serverID int) (session, rpt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[serverID]
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

// ReadProcessLog returns new stdout/stderr captured since offset.
func (m *Manager) ReadProcessLog(serverID, offset int) (string, int) {
	m.mu.Lock()
	inst, ok := m.instances[serverID]
	m.mu.Unlock()
	if !ok || inst.sessionLog == nil {
		return "", offset
	}
	return inst.sessionLog.ReadFrom(offset)
}

// ActiveSessionLogPath returns the on-disk session log for a running server.
func (m *Manager) ActiveSessionLogPath(serverID int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[serverID]; ok {
		return inst.sessionLogPath
	}
	return ""
}

func (m *Manager) watchServer(serverID int, gen uint64) {
	const pollInterval = 2 * time.Second
	const deadThreshold = 2

	consecutiveDead := 0
	for {
		time.Sleep(pollInterval)

		m.mu.Lock()
		inst, tracked := m.instances[serverID]
		sameGen := tracked && inst.gen == gen
		var proc *managedProc
		var pid int
		if sameGen {
			proc = inst.proc
			pid = inst.pid
		}
		m.mu.Unlock()
		if !tracked || !sameGen {
			// Stop/Restart already replaced or removed this instance.
			return
		}

		srv, err := database.GetServer(serverID)
		if err != nil {
			return
		}

		// Prefer the Wait() result: once reaped, the PID is gone. Port fallback
		// still covers Arma leaving a child bound after the launcher exits.
		if proc != nil && proc.exited() {
			if IsPortInUse(srv.GamePort) {
				consecutiveDead = 0
				m.broadcast()
				continue
			}
		} else if ServerAlive(pid, srv.GamePort) {
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
	inst, ok := m.instances[serverID]
	if !ok || inst.gen != gen {
		m.mu.Unlock()
		return
	}
	if inst.proc != nil {
		_ = inst.proc.wait(30 * time.Second)
	}
	m.closeInstanceLog(inst)
	delete(m.instances, serverID)
	m.mu.Unlock()

	m.StopAllHeadless(serverID)

	srv, _ := database.GetServer(serverID)
	activeRPT := ""
	if srv != nil {
		activeRPT = srv.ActiveRPT
	}
	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, activeRPT)
	ReleaseServerMemoryLimit(serverID)
	if pins, err := database.ClearServerPins(serverID); err == nil {
		contentver.GCReleasedPins(pins)
	}
	m.broadcast()
}

func processRunning(pid int) bool {
	ok, _ := osFindProcess(pid)
	return ok && !processIsZombie(pid)
}
