package server

import (
	"fmt"
	"log"
	"sync"
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
	serverID  int
	pid       int
	startedAt time.Time
	rptPath   string
	logBuf    *logBuffer
}

// Default is the process-wide server manager.
var Default = &Manager{
	instances: make(map[int]*runningInstance),
	subs:      make(map[chan AllStatusSnapshot]struct{}),
}

// Init reconciles DB state on startup.
func Init() {
	if err := database.ReconcileServerStatuses(); err != nil {
		log.Printf("[SERVER] failed to reconcile statuses: %v", err)
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
	if !IsPortAvailable(srv.GamePort) {
		return fmt.Errorf("game port %d is not available", srv.GamePort)
	}
	if !IsPortAvailable(srv.SteamPort) {
		return fmt.Errorf("steam port %d is not available", srv.SteamPort)
	}

	if err := ProvisionServer(serverID); err != nil {
		return err
	}

	launch, err := BuildLaunchArgs(serverID)
	if err != nil {
		return err
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStarting, 0, "")

	startTime := time.Now()
	logBuf := newLogBuffer(512 * 1024)
	pid, err := startProcess(launch.Binary, launch.Args, launch.WorkingDir, logBuf)
	if err != nil {
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		return err
	}

	rptPath, _ := WaitForRPT(srv.Slug, startTime.Add(-time.Second), 30*time.Second)
	if rptPath == "" {
		rptPath = ResolveRPTPath(srv.Slug, "")
	}

	m.mu.Lock()
	m.instances[serverID] = &runningInstance{
		serverID:  serverID,
		pid:       pid,
		startedAt: startTime,
		rptPath:   rptPath,
		logBuf:    logBuf,
	}
	m.mu.Unlock()

	_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, pid, rptPath)
	m.broadcast()

	go m.waitProcess(serverID, pid)
	return nil
}

// Stop stops a running server.
func (m *Manager) Stop(serverID int) error {
	m.mu.Lock()
	inst, ok := m.instances[serverID]
	m.mu.Unlock()

	if !ok {
		srv, err := database.GetServer(serverID)
		if err != nil {
			return err
		}
		if srv.PID > 0 && processRunning(srv.PID) {
			if err := stopProcess(srv.PID); err != nil {
				return err
			}
		}
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, srv.ActiveRPT)
		m.broadcast()
		return nil
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopping, inst.pid, inst.rptPath)
	m.broadcast()

	if err := stopProcess(inst.pid); err != nil {
		return err
	}
	return nil
}

// Restart stops then starts a server.
func (m *Manager) Restart(serverID int) error {
	_ = m.Stop(serverID)
	time.Sleep(2 * time.Second)
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
	if !ok || inst.logBuf == nil {
		return "", offset
	}
	return inst.logBuf.ReadFrom(offset)
}

func (m *Manager) waitProcess(serverID, pid int) {
	waitProcess(pid)

	m.mu.Lock()
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
	return ok
}
