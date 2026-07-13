package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"arma3modmanager/internal/database"
)

// StatusSnapshot is the runtime status of one server.
type StatusSnapshot struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	PID       int       `json:"pid"`
	ActiveRPT string    `json:"activeRpt"`
	Uptime    int64     `json:"uptime"` // seconds, 0 if not running
	StartedAt time.Time `json:"startedAt,omitempty"`
}

// AllStatusSnapshot is broadcast to SSE subscribers.
type AllStatusSnapshot struct {
	Servers []StatusSnapshot `json:"servers"`
}

// Manager controls running Arma server processes.
type Manager struct {
	mu        sync.Mutex
	instances map[int]*runningInstance

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
}

// Default is the process-wide server manager.
var Default = &Manager{
	instances: make(map[int]*runningInstance),
	subs:      make(map[chan AllStatusSnapshot]struct{}),
}

var nextInstanceGen uint64

// Init reconciles DB state on startup.
func Init() {
	ReconcileStatuses()
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
		alive := ServerAlive(s.PID, s.GamePort)
		switch s.Status {
		case database.ServerStatusRunning, database.ServerStatusStarting:
			if !alive {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, 0, s.ActiveRPT)
			}
		case database.ServerStatusStopping:
			if alive && s.PID > 0 && processRunning(s.PID) {
				if err := stopProcess(s.PID); err != nil && ServerAlive(s.PID, s.GamePort) {
					// Kill failed and process still up — unknown so Start/Stop stay usable.
					_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, s.PID, s.ActiveRPT)
					continue
				}
			}
			if ServerAlive(s.PID, s.GamePort) {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, s.PID, s.ActiveRPT)
				continue
			}
			_ = database.UpdateServerStatus(s.ID, database.ServerStatusStopped, 0, s.ActiveRPT)
		case database.ServerStatusUnknown:
			if !alive {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusStopped, 0, s.ActiveRPT)
			}
		}
	}
}

// Snapshot returns current status for all servers.
func (m *Manager) Snapshot() AllStatusSnapshot {
	servers, err := database.GetServers()
	if err != nil {
		return AllStatusSnapshot{Servers: []StatusSnapshot{}}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var snaps []StatusSnapshot
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
		}
		snaps = append(snaps, snap)
	}
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
	if !IsBranchInstalled(branch.InstallPath) {
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

	if err := ProvisionServer(serverID); err != nil {
		return err
	}

	launch, err := BuildLaunchArgs(serverID)
	if err != nil {
		return err
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStarting, 0, "")
	m.broadcast()

	startTime := time.Now()
	if err := os.MkdirAll(filepath.Join(InstanceDir(srv.Slug), "logs"), 0755); err != nil {
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	sessionLogPath := filepath.Join(InstanceDir(srv.Slug), "logs",
		fmt.Sprintf("session-%s.log", startTime.Format("2006-01-02_150405")))
	sessionFile, err := os.OpenFile(sessionLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return fmt.Errorf("failed to create session log: %w", err)
	}
	sessionLog := newSessionLogWriter(sessionFile, 512*1024)

	proc, err := startProcess(launch.Binary, launch.Args, launch.WorkingDir, sessionLog)
	if err != nil {
		_ = sessionLog.Close()
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
		return err
	}

	// Confirm the process did not die immediately (e.g. port still held on UDP).
	if err := verifyProcessStartup(proc, srv.GamePort, 8*time.Second); err != nil {
		_ = stopProcess(proc.pid)
		_ = proc.wait(5 * time.Second)
		_ = sessionLog.Close()
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
	}
	m.mu.Unlock()

	_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, proc.pid, rptPath)
	m.broadcast()

	go m.watchServer(serverID, gen)
	go m.resolveRPT(serverID, gen, startTime)
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

	if err := stopProcess(inst.pid); err != nil {
		if !inst.proc.exited() && ServerAlive(inst.pid, gamePort) {
			// Still running — revert so status cannot stay stuck on "stopping".
			_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, inst.pid, inst.rptPath)
			m.broadcast()
			return err
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
	m.broadcast()
	return nil
}

// stopOrphan clears status for a server not tracked in-memory (e.g. after restart).
func (m *Manager) stopOrphan(serverID int) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	if srv.PID > 0 && processRunning(srv.PID) {
		if err := stopProcess(srv.PID); err != nil && ServerAlive(srv.PID, srv.GamePort) {
			return err
		}
	}
	_ = waitForPortsFree(srv.GamePort, srv.SteamPort, 15*time.Second)
	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, srv.ActiveRPT)
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
				continue
			}
		} else if ServerAlive(pid, srv.GamePort) {
			consecutiveDead = 0
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

	srv, _ := database.GetServer(serverID)
	activeRPT := ""
	if srv != nil {
		activeRPT = srv.ActiveRPT
	}
	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, activeRPT)
	m.broadcast()
}

func processRunning(pid int) bool {
	ok, _ := osFindProcess(pid)
	return ok && !processIsZombie(pid)
}
