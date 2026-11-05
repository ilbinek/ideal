package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
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
	Op        string                   `json:"op,omitempty"` // lifecycle operation in progress

	// Automation state, filled in by the status decorator.
	Autostart     bool  `json:"autostart,omitempty"`
	RetryAt       int64 `json:"retryAt,omitempty"`
	Failures      int   `json:"failures,omitempty"`
	GaveUp        bool  `json:"gaveUp,omitempty"`
	UserStopped   bool  `json:"userStopped,omitempty"`
	NextRestartAt int64 `json:"nextRestartAt,omitempty"`
	Paused        bool  `json:"paused,omitempty"`
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

	opMu sync.Mutex
	ops  map[int]*serverOp

	hookMu       sync.Mutex
	exitObserver func(ExitEvent)
	lastPins     map[int]string // content each server last ran, to spot changes

	statusDecorator func(*StatusSnapshot)
	resumingStops   map[int]bool

	// Test seams; nil means the real implementation.
	launcher        func(context.Context, int) (*launchedServer, error)
	terminator      func(context.Context, *runningInstance, *serverOp) error
	headlessStarter func(context.Context, int, int) error
	watchInterval   time.Duration
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
	state          instState // guarded by Manager.mu

	// Headless auto-start tracking, so a stop can cancel it and wait for it.
	hcCtx    context.Context
	hcCancel context.CancelFunc
	hcWG     *sync.WaitGroup
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
	resumeStops := ReconcileStatuses()
	ReconcileHeadlessStatuses()
	Default.adoptOrphans()
	Default.hookMu.Lock()
	Default.resumingStops = make(map[int]bool, len(resumeStops))
	for _, id := range resumeStops {
		Default.resumingStops[id] = true
	}
	Default.hookMu.Unlock()
	for _, id := range resumeStops {
		go func(id int) {
			defer func() {
				Default.hookMu.Lock()
				delete(Default.resumingStops, id)
				Default.hookMu.Unlock()
			}()
			if err := Default.Stop(id, ActorSystem); err != nil {
				log.Printf("[SERVER] resuming interrupted stop of server %d: %v", id, err)
			}
		}(id)
	}
	// Drop unused old generations after adopt so pins for live servers are kept.
	contentver.GCAllOrphans()
	// Check .pbo/.bisign/.bikey state for mods downloaded before this was tracked.
	go BackfillModSignatures()
}

// ReconcileStatuses settles orphaned DB statuses on startup. A server stays
// running/starting only if its recorded process, its container, or a process
// provably its own (holding its game port) is still alive; a port held by anything
// else doesn't count. Transitional "stopping" is always recovered so a mid-stop
// reboot cannot trap the UI.
func ReconcileStatuses() (resumeStops []int) {
	servers, err := database.GetServers()
	if err != nil {
		log.Printf("[SERVER] failed to reconcile statuses: %v", err)
		return nil
	}
	for i := range servers {
		s := servers[i]
		dockerUp := dockerContainerRunning(s.ID)
		identity := identifyServerPID(&s)
		if !dockerUp && identity != identityMatch && identity != identityUnknown &&
			s.Status != database.ServerStatusStopped {
			// The recorded PID is gone, but a child of this server may hold the port.
			portSt, owner := inspectGamePort(&s)
			if portSt == portOwned {
				createMs := captureCreateMs(owner)
				_ = database.UpdateServerProcess(s.ID, s.Status, owner, createMs, s.ActiveRPT)
				s.PID, s.PIDCreateMs = owner, createMs
				servers[i] = s
				identity = identityMatch
			} else if portSt != portFree {
				holder := describePortHolder(s.GamePort)
				log.Printf("[SERVER] game port %d of server %d (%s) is in use by something else%s",
					s.GamePort, s.ID, s.Slug, holder)
				recordEvent(s.ID, database.EventPortHeld, ActorSystem,
					fmt.Sprintf("game port %d is in use by something else%s", s.GamePort, holder))
			}
		}
		alive := dockerUp || identity == identityMatch
		if !dockerUp && identity == identityUnknown {
			// A live process we can't inspect: don't adopt it, don't kill it, and
			// don't call it stopped. Stop explains, and a later run re-checks.
			log.Printf("[SERVER] cannot verify pid=%d for server %d (%s); leaving it alone", s.PID, s.ID, s.Slug)
			recordEvent(s.ID, database.EventIdentityUnknown, ActorSystem,
				fmt.Sprintf("could not verify that process %d belongs to this server, so it was left alone", s.PID))
			if s.Status != database.ServerStatusStopped {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, s.PID, s.ActiveRPT)
			}
			continue
		}
		if identity == identityMismatch {
			log.Printf("[SERVER] pid=%d for server %d (%s) now belongs to another process", s.PID, s.ID, s.Slug)
		}
		switch s.Status {
		case database.ServerStatusRunning, database.ServerStatusStarting:
			if !alive {
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusUnknown, 0, s.ActiveRPT)
				if pins, err := database.ClearServerPins(s.ID); err == nil {
					contentver.GCReleasedPins(pins)
				}
			}
		case database.ServerStatusStopping:
			if alive {
				// Finish it in the background, after adoption, so a graceful stop that can
				// take over a minute doesn't hold up startup. Marked running so adoption
				// picks it up.
				_ = database.UpdateServerStatus(s.ID, database.ServerStatusRunning, s.PID, s.ActiveRPT)
				resumeStops = append(resumeStops, s.ID)
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
	return resumeStops
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
	for i := range servers {
		s := servers[i]
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
		} else if identifyServerPID(&s) == identityMatch {
			pid = s.PID
			proc = adoptProcess(s.PID)
			// Backfill rows from before start times were recorded, or after a clock step.
			if ms := captureCreateMs(pid); ms > 0 && ms != s.PIDCreateMs {
				_ = database.UpdateServerProcess(s.ID, s.Status, pid, ms, s.ActiveRPT)
			}
		} else {
			continue
		}

		gen := atomic.AddUint64(&nextInstanceGen, 1)
		hcCtx, hcCancel := context.WithCancel(context.Background())
		m.instances[s.ID] = &runningInstance{
			serverID:    s.ID,
			gen:         gen,
			pid:         pid,
			proc:        proc,
			startedAt:   time.Now(),
			rptPath:     s.ActiveRPT,
			dockerMode:  dockerMode,
			containerID: containerID,
			hcCtx:       hcCtx,
			hcCancel:    hcCancel,
			hcWG:        &sync.WaitGroup{},
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
		if !parentDocker {
			// Docker exec PIDs may be namespace-local, so only native HCs can be checked.
			if identifyHeadlessPID(&c) != identityMatch {
				continue
			}
			if ms := captureCreateMs(c.PID); ms > 0 && ms != c.PIDCreateMs {
				_ = database.UpdateHeadlessProcess(c.ServerID, c.Slot, c.Status, c.PID, ms, c.ActiveRPT)
			}
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

	m.hookMu.Lock()
	decorate := m.statusDecorator
	m.hookMu.Unlock()

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
			Op:        string(m.CurrentOp(s.ID)),
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
		if decorate != nil {
			decorate(&snap)
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

func (m *Manager) closeInstanceLog(inst *runningInstance) {
	if inst == nil || inst.sessionLog == nil {
		return
	}
	_ = inst.sessionLog.Close()
	inst.sessionLog = nil
}

// latestSessionLogPath returns the newest session-*.log under the instance logs dir.
func latestSessionLogPath(slug string) string {
	logs, err := ListLogs(slug)
	if err != nil {
		return ""
	}
	for _, e := range logs {
		name := strings.ToLower(e.Name)
		if strings.HasPrefix(name, "session-") && strings.HasSuffix(name, ".log") {
			return e.Path
		}
	}
	return filepath.Join(InstanceDir(slug), "logs",
		fmt.Sprintf("session-%s.log", time.Now().Format("2006-01-02_150405")))
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

func processRunning(pid int) bool {
	ok, _ := osFindProcess(pid)
	return ok && !processIsZombie(pid)
}
