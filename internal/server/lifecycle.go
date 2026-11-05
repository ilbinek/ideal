package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
)

// instState records who owns an instance's shutdown. The first claim wins, which
// is how an exit we asked for is told apart from one we didn't.
type instState int

const (
	instRunning instState = iota
	instStopRequested
	instExited
)

// defaultWatchInterval is how often watchServer checks a server.
const defaultWatchInterval = 2 * time.Second

// ExitEvent describes a server process going away without Ideal asking it to.
type ExitEvent struct {
	ServerID  int
	StartedAt time.Time
	At        time.Time
	ExitCode  int
	HasCode   bool
}

// ErrAlreadyRunning means a start found the server already up.
var ErrAlreadyRunning = errors.New("server is already running")

// launchedServer is a verified, running process that isn't registered yet.
type launchedServer struct {
	proc           *managedProc
	startTime      time.Time
	rptPath        string
	sessionLog     *sessionLogWriter
	sessionLogPath string
	dockerMode     bool
	containerID    string
	resolveRPT     bool
}

// SetExitObserver registers a callback for unrequested exits. It must not block.
func (m *Manager) SetExitObserver(fn func(ExitEvent)) {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.exitObserver = fn
}

func (m *Manager) notifyExit(ev ExitEvent) {
	m.hookMu.Lock()
	fn := m.exitObserver
	m.hookMu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

func (m *Manager) launchFn() func(context.Context, int) (*launchedServer, error) {
	if m.launcher != nil {
		return m.launcher
	}
	return m.launchServer
}

func (m *Manager) terminateFn() func(context.Context, *runningInstance, *serverOp) error {
	if m.terminator != nil {
		return m.terminator
	}
	return terminateInstance
}

func (m *Manager) headlessStartFn() func(context.Context, int, int) error {
	if m.headlessStarter != nil {
		return m.headlessStarter
	}
	return m.startHeadlessCtx
}

// Start provisions and launches a server. It fails with *BusyError if another
// lifecycle operation is in progress.
func (m *Manager) Start(serverID int, by Actor) error {
	op, running := m.tryBeginOp(serverID, OpStart, by, func(claimed bool) {
		if claimed && by != ActorSystem {
			logIntentError(serverID, database.RecordManualStart(serverID))
		}
	})
	if op == nil {
		return &BusyError{Op: running.kind}
	}
	defer m.endOp(serverID, op)
	err := m.startInner(op, serverID)
	if err != nil {
		recordEvent(serverID, database.EventStartFailed, by, err.Error())
		return err
	}
	recordEvent(serverID, database.EventStarted, by, "")
	return nil
}

// StartSystem starts a server on Ideal's own initiative. guard runs once the
// server's operation lock is held, so it sees the final word on whether this should
// still happen; a person's Stop can't slip in between the check and the start.
func (m *Manager) StartSystem(serverID int, guard func() error) error {
	op, running := m.tryBeginOp(serverID, OpStart, ActorSystem, nil)
	if op == nil {
		return &BusyError{Op: running.kind}
	}
	defer m.endOp(serverID, op)
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	return m.startInner(op, serverID)
}

// RestartPlanned restarts a server on a schedule, with the same guard as
// StartSystem. attempted says whether the start half ran.
func (m *Manager) RestartPlanned(serverID int, guard func() error) (attempted bool, err error) {
	op, running := m.tryBeginOp(serverID, OpRestart, ActorSystem, nil)
	if op == nil {
		return false, &BusyError{Op: running.kind}
	}
	defer m.endOp(serverID, op)
	if guard != nil {
		if err := guard(); err != nil {
			return false, err
		}
	}
	return m.restartInner(op, serverID)
}

// Running reports whether a server is being tracked as running, and since when.
func (m *Manager) Running(serverID int) (bool, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[serverID]
	if !ok || inst.state != instRunning {
		return false, time.Time{}
	}
	return true, inst.startedAt
}

// ResumingStop reports whether a stop interrupted by Ideal dying is being finished.
func (m *Manager) ResumingStop(serverID int) bool {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	return m.resumingStops[serverID]
}

// SetStatusDecorator registers a callback that adds to each status snapshot. It's
// called with the manager's lock held, so it must not block or take locks.
func (m *Manager) SetStatusDecorator(fn func(*StatusSnapshot)) {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.statusDecorator = fn
}

// Broadcast pushes current status to subscribers.
func (m *Manager) Broadcast() {
	m.broadcast()
}

func logIntentError(serverID int, err error) {
	if err != nil {
		log.Printf("[SERVER] could not record intent for server %d: %v", serverID, err)
	}
}

// recordEvent adds to a server's history. A failure here mustn't affect the server.
func recordEvent(serverID int, kind string, by Actor, detail string) {
	if err := database.AddServerEvent(serverID, kind, string(by), detail); err != nil {
		log.Printf("[SERVER] could not record %s event for server %d: %v", kind, serverID, err)
	}
}

func pinsSignature(pins []database.ServerContentPin) string {
	parts := make([]string, 0, len(pins))
	for _, p := range pins {
		parts = append(parts, fmt.Sprintf("%s/%d/%d", p.Kind, p.RefID, p.Generation))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// noteContentChange records when a server came up on different mods or game files
// than its last run, which is easy to miss when the restart was automatic.
func (m *Manager) noteContentChange(serverID int) {
	pins, err := database.GetServerPins(serverID)
	if err != nil || len(pins) == 0 {
		return
	}
	current := pinsSignature(pins)
	m.hookMu.Lock()
	previous := m.lastPins[serverID]
	m.hookMu.Unlock()
	if previous != "" && previous != current {
		recordEvent(serverID, database.EventContentChanged, ActorSystem, "started on newer mods or game files than its last run")
	}
}

func (m *Manager) startInner(op *serverOp, serverID int) error {
	m.mu.Lock()
	if _, running := m.instances[serverID]; running {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	m.mu.Unlock()

	started, err := m.launchFn()(op.ctx, serverID)
	if err != nil {
		return err
	}

	gen := atomic.AddUint64(&nextInstanceGen, 1)
	hcCtx, hcCancel := context.WithCancel(context.Background())
	inst := &runningInstance{
		serverID:       serverID,
		gen:            gen,
		pid:            started.proc.pid,
		proc:           started.proc,
		startedAt:      started.startTime,
		rptPath:        started.rptPath,
		sessionLog:     started.sessionLog,
		sessionLogPath: started.sessionLogPath,
		dockerMode:     started.dockerMode,
		containerID:    started.containerID,
		hcCtx:          hcCtx,
		hcCancel:       hcCancel,
		hcWG:           &sync.WaitGroup{},
	}

	m.mu.Lock()
	m.instances[serverID] = inst
	m.mu.Unlock()

	_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, started.proc.pid, started.rptPath)
	m.broadcast()
	m.noteContentChange(serverID)

	go m.watchServer(serverID, gen)
	if started.resolveRPT {
		go m.resolveRPT(serverID, gen, started.startTime)
	}

	// Start configured headless clients after the dedicated server is up. Tracked
	// on the instance so a stop can cancel this and wait for it, rather than have
	// clients launching behind it.
	inst.hcWG.Add(1)
	go func() {
		defer inst.hcWG.Done()
		select {
		case <-time.After(2 * time.Second):
		case <-hcCtx.Done():
			return
		}
		m.startAllHeadlessCtx(hcCtx, serverID)
	}()
	return nil
}

// launchServer does everything up to a verified running process: checks,
// provisioning, spawning. It backs out cleanly when ctx is cancelled.
func (m *Manager) launchServer(ctx context.Context, serverID int) (*launchedServer, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return nil, err
	}
	if !BranchIsInstalled(branch) {
		return nil, fmt.Errorf("branch %q is not installed", branch.Name)
	}

	conflicts, err := database.GetServersUsingPort(srv.GamePort, srv.SteamPort, serverID)
	if err != nil {
		return nil, err
	}
	for _, c := range conflicts {
		if c.Status == database.ServerStatusRunning || c.Status == database.ServerStatusStarting {
			return nil, fmt.Errorf("port conflict with server %q", c.Name)
		}
	}
	if err := waitForPortsFree(ctx, srv.GamePort, srv.SteamPort, 30*time.Second); err != nil {
		if ctx.Err() != nil {
			return nil, errStartCancelled(ctx.Err())
		}
		return nil, err
	}

	if err := FindRunningAffinityConflict(serverID, ParseAffinity(srv.CPUAffinity)); err != nil {
		return nil, err
	}

	pins, err := contentver.CollectPinsForServer(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to collect content pins: %w", err)
	}
	if err := database.ReplaceServerPins(serverID, pins); err != nil {
		return nil, fmt.Errorf("failed to pin content versions: %w", err)
	}

	if err := ProvisionServer(serverID); err != nil {
		_, _ = database.ClearServerPins(serverID)
		return nil, err
	}

	launch, err := BuildLaunchArgs(serverID)
	if err != nil {
		_, _ = database.ClearServerPins(serverID)
		return nil, err
	}

	// A crash can leave only BattlEye's active copy of its config behind.
	if err := restoreBEConfig(srv); err != nil {
		log.Printf("[SERVER] could not restore BattlEye config for %d: %v", serverID, err)
	}

	// Last chance to back out before anything is spawned.
	if err := ctx.Err(); err != nil {
		_, _ = database.ClearServerPins(serverID)
		return nil, errStartCancelled(err)
	}

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStarting, 0, "")
	m.broadcast()

	abort := func() {
		_, _ = database.ClearServerPins(serverID)
		_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, "")
		m.broadcast()
	}

	startTime := time.Now()
	if err := os.MkdirAll(filepath.Join(InstanceDir(srv.Slug), "logs"), 0755); err != nil {
		abort()
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}
	sessionLogPath := filepath.Join(InstanceDir(srv.Slug), "logs",
		fmt.Sprintf("session-%s.log", startTime.Format("2006-01-02_150405")))
	sessionFile, err := os.OpenFile(sessionLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		abort()
		return nil, fmt.Errorf("failed to create session log: %w", err)
	}
	sessionLog := newSessionLogWriter(sessionFile, 512*1024)

	useDocker, dockerErr := dockerModeForStart()
	if dockerErr != nil {
		_ = sessionLog.Close()
		abort()
		return nil, dockerErr
	}
	var (
		proc        *managedProc
		containerID string
		startErr    error
	)
	if useDocker {
		// Close Ideal's write handle so the container can append freely; live
		// tail and historic reads use the on-disk session file under logs/.
		_ = sessionLog.Close()
		sessionLog = nil
		extraMounts := CollectDockerExtraMounts(serverID)
		containerID, proc, startErr = startDockerServer(
			serverID, launch.Binary, launch.Args, launch.WorkingDir, sessionLogPath,
			srv.CPUAffinity, srv.MemoryLimitMB, extraMounts,
		)
	} else {
		proc, startErr = startProcess(launch.Binary, launch.Args, launch.WorkingDir, sessionLog)
	}
	if startErr != nil {
		_ = sessionLog.Close()
		abort()
		return nil, startErr
	}

	// Persist the PID right away: if Ideal dies during the verify window below,
	// the next run can still find and adopt this process.
	createMs := int64(0)
	if !useDocker {
		createMs = captureCreateMs(proc.pid)
	}
	_ = database.UpdateServerProcess(serverID, database.ServerStatusStarting, proc.pid, createMs, "")

	if !useDocker {
		if err := ApplyServerAffinity(proc.pid, srv.CPUAffinity); err != nil {
			log.Printf("[SERVER] affinity warning for %d: %v", serverID, err)
		}
		if err := ApplyServerMemoryLimit(serverID, proc.pid, srv.MemoryLimitMB); err != nil {
			log.Printf("[SERVER] memory limit warning for %d: %v", serverID, err)
		}
	}

	// Confirm the process did not die immediately (e.g. port still held on UDP).
	verified, err := verifyProcessStartup(ctx, proc, srv, 8*time.Second)
	if err != nil {
		if useDocker {
			_ = stopDockerContainer(serverID, sessionLogPath)
		} else {
			_ = stopProcess(proc.pid)
		}
		_ = proc.wait(5 * time.Second)
		_ = sessionLog.Close()
		abort()
		log.Printf("[SERVER] start failed for %d: %v", serverID, err)
		return nil, err
	}
	if verified != proc {
		// The launched process handed off to a child, which is the real server now.
		proc = verified
		_ = database.UpdateServerProcess(serverID, database.ServerStatusStarting, proc.pid, captureCreateMs(proc.pid), "")
		if err := ApplyServerAffinity(proc.pid, srv.CPUAffinity); err != nil {
			log.Printf("[SERVER] affinity warning for %d: %v", serverID, err)
		}
		if err := ApplyServerMemoryLimit(serverID, proc.pid, srv.MemoryLimitMB); err != nil {
			log.Printf("[SERVER] memory limit warning for %d: %v", serverID, err)
		}
	}

	// Linux Arma prints RPT to stdout (captured into the session log under Docker).
	// Point ActiveRPT at that file so live tail and the log list stay in sync.
	rptPath := ResolveRPTPath(srv.Slug, "")
	if useDocker {
		rptPath = sessionLogPath
	}
	return &launchedServer{
		proc:           proc,
		startTime:      startTime,
		rptPath:        rptPath,
		sessionLog:     sessionLog,
		sessionLogPath: sessionLogPath,
		dockerMode:     useDocker,
		containerID:    containerID,
		resolveRPT:     !useDocker,
	}, nil
}

func waitForPortsFree(ctx context.Context, gamePort, steamPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		gameFree := IsPortAvailable(gamePort)
		steamFree := IsPortAvailable(steamPort)
		if gameFree && steamFree {
			return nil
		}
		if time.Now().After(deadline) {
			if !gameFree {
				return fmt.Errorf("game port %d is not available%s", gamePort, describePortHolder(gamePort))
			}
			return fmt.Errorf("steam port %d is not available%s", steamPort, describePortHolder(steamPort))
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// verifyProcessStartup ensures the child did not exit right away. Prefer seeing
// the game port bind; if Arma is still starting after timeout, allow it. It returns
// the process to track, which differs from proc if a child of it took over.
func verifyProcessStartup(ctx context.Context, proc *managedProc, srv *database.Server, timeout time.Duration) (*managedProc, error) {
	deadline := time.Now().Add(timeout)
	sawAlive := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, errStartCancelled(err)
		}
		if !proc.exited() {
			sawAlive = true
			// Both ports were free before launch, so a busy port now is this process.
			if IsPortInUse(srv.GamePort) {
				return proc, nil
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		successor, err := successorAfterExit(srv)
		if successor != nil || err != nil {
			return successor, err
		}
		if sawAlive {
			return nil, errExitedAfterStart
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !proc.exited() {
		return proc, nil
	}
	successor, err := successorAfterExit(srv)
	if successor != nil || err != nil {
		return successor, err
	}
	return nil, errExitedAfterStart
}

var errExitedAfterStart = errors.New("server process exited immediately after start (check session log / ports)")

// successorAfterExit looks at the game port after the launched process exited. A
// child of this server holding it takes over; anything else holding it is an error.
// A free port returns nothing, leaving the caller to decide.
func successorAfterExit(srv *database.Server) (*managedProc, error) {
	state, owner := inspectGamePort(srv)
	switch state {
	case portOwned:
		return adoptProcess(owner), nil
	case portFree:
		return nil, nil
	default:
		return nil, fmt.Errorf("server process exited after start and game port %d is in use%s", srv.GamePort, describePortHolder(srv.GamePort))
	}
}

// Stop shuts a server down, preempting a start that is still in progress and
// joining a stop that is already running.
func (m *Manager) Stop(serverID int, by Actor) error {
	return m.requestStop(serverID, by, false)
}

// ForceStop kills a server immediately, overriding any graceful stop underway.
func (m *Manager) ForceStop(serverID int, by Actor) error {
	return m.requestStop(serverID, by, true)
}

func (m *Manager) requestStop(serverID int, by Actor, force bool) error {
	if force {
		recordEvent(serverID, database.EventForceStop, by, "")
	}
	for {
		// Recorded before any signal goes out, and whether or not this claims the op:
		// a person wants this server off from this moment on.
		op, running := m.tryBeginOp(serverID, OpStop, by, func(bool) {
			if by != ActorSystem {
				logIntentError(serverID, database.RecordManualStop(serverID))
			}
		})
		if op != nil {
			op.forced.Store(force)
			defer m.endOp(serverID, op)
			wasUp := m.serverIsUp(serverID)
			err := m.stopInner(op, serverID)
			if err != nil {
				recordEvent(serverID, database.EventStopFailed, by, err.Error())
			} else if wasUp {
				recordEvent(serverID, database.EventStopped, by, "")
			}
			return err
		}
		switch running.kind {
		case OpDelete:
			return &BusyError{Op: running.kind}
		case OpStart:
			running.cancel()
		case OpRestart:
			running.skipStart.Store(true)
			if opPhase(running.phase.Load()) == phaseStarting || force {
				running.forced.Store(force)
				running.cancel()
			}
		case OpStop:
			if force {
				running.forced.Store(true)
				running.cancel()
			}
		}
		<-running.done
	}
}

// serverIsUp reports whether there's anything for a stop to do. It tells a real stop
// from one that merely joined a stop that has already finished.
func (m *Manager) serverIsUp(serverID int) bool {
	m.mu.Lock()
	_, tracked := m.instances[serverID]
	m.mu.Unlock()
	if tracked {
		return true
	}
	srv, err := database.GetServer(serverID)
	return err == nil && srv.Status != database.ServerStatusStopped
}

// claimStop marks the instance as being stopped by an operation. It reports the
// instance, and whether the watcher got there first because it already exited.
func (m *Manager) claimStop(serverID int) (inst *runningInstance, alreadyExited bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[serverID]
	if !ok {
		return nil, false
	}
	if inst.state == instExited {
		return inst, true
	}
	inst.state = instStopRequested
	return inst, false
}

func (m *Manager) claimExit(serverID int, gen uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[serverID]
	if !ok || inst.gen != gen || inst.state != instRunning {
		return false
	}
	inst.state = instExited
	return true
}

// stopHeadlessFor cancels any pending auto-start, waits for it to finish, then
// stops the clients. Only after this can no new client appear.
func (m *Manager) stopHeadlessFor(serverID int, inst *runningInstance) {
	if inst != nil && inst.hcCancel != nil {
		inst.hcCancel()
		inst.hcWG.Wait()
	}
	m.StopAllHeadless(serverID)
}

// removeInstance drops the instance from tracking. Idempotent by generation.
func (m *Manager) removeInstance(inst *runningInstance) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.instances[inst.serverID]
	if !ok || cur.gen != inst.gen {
		return false
	}
	m.closeInstanceLog(cur)
	delete(m.instances, inst.serverID)
	return true
}

func (m *Manager) markStopped(serverID int, activeRPT string) {
	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopped, 0, activeRPT)
	ReleaseServerMemoryLimit(serverID)
	if pins, err := database.ClearServerPins(serverID); err == nil {
		if len(pins) > 0 {
			m.hookMu.Lock()
			if m.lastPins == nil {
				m.lastPins = make(map[int]string)
			}
			m.lastPins[serverID] = pinsSignature(pins)
			m.hookMu.Unlock()
		}
		contentver.GCReleasedPins(pins)
	}
	m.broadcast()
}

func (m *Manager) stopInner(op *serverOp, serverID int) error {
	inst, alreadyExited := m.claimStop(serverID)
	if inst == nil {
		m.StopAllHeadless(serverID)
		return m.stopOrphan(serverID)
	}
	if alreadyExited {
		// It died on its own just before we got here, so it still counts as an exit
		// nobody asked for. The stop itself has nothing left to do.
		m.finishExited(inst)
		return nil
	}

	m.stopHeadlessFor(serverID, inst)

	_ = database.UpdateServerStatus(serverID, database.ServerStatusStopping, inst.pid, inst.rptPath)
	m.broadcast()

	srv, _ := database.GetServer(serverID)
	gamePort, steamPort := 0, 0
	if srv != nil {
		gamePort = srv.GamePort
		steamPort = srv.SteamPort
	}

	if stopErr := m.terminateFn()(op.ctx, inst, op); stopErr != nil {
		stillAlive := false
		if inst.dockerMode {
			stillAlive = dockerContainerRunning(serverID)
		} else {
			stillAlive = !inst.proc.exited() && processRunning(inst.pid)
		}
		if stillAlive {
			// Still running — revert so status cannot stay stuck on "stopping".
			m.mu.Lock()
			inst.state = instRunning
			m.mu.Unlock()
			_ = database.UpdateServerStatus(serverID, database.ServerStatusRunning, inst.pid, inst.rptPath)
			m.broadcast()
			return stopErr
		}
		// Process already gone; treat as a successful stop.
	}

	// Wait until stdout/stderr pipes are fully drained before closing the log.
	_ = inst.proc.wait(30 * time.Second)
	m.removeInstance(inst)

	if srv != nil && !inst.dockerMode {
		stopOwnedPortHolder(srv)
	}
	if gamePort > 0 {
		_ = waitForPortsFree(context.Background(), gamePort, steamPort, 30*time.Second)
	}
	m.markStopped(serverID, inst.rptPath)
	return nil
}

// stopOrphan clears status for a server not tracked in-memory (e.g. after restart).
func (m *Manager) stopOrphan(serverID int) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	if _, _, running, _ := findDockerContainer(serverID); running || dockerContainerRunning(serverID) {
		_ = stopDockerContainer(serverID, latestSessionLogPath(srv.Slug))
	} else {
		switch identifyServerPID(srv) {
		case identityMatch:
			if err := stopProcess(srv.PID); err != nil && processRunning(srv.PID) {
				return err
			}
		case identityUnknown:
			return fmt.Errorf("cannot verify that process %d belongs to this server, so it was not stopped", srv.PID)
		case identityMismatch:
			log.Printf("[SERVER] not stopping pid=%d: it no longer belongs to server %d", srv.PID, serverID)
		}
		stopOwnedPortHolder(srv)
	}
	_ = waitForPortsFree(context.Background(), srv.GamePort, srv.SteamPort, 15*time.Second)
	m.markStopped(serverID, srv.ActiveRPT)
	return nil
}

// Restart stops then starts a server.
func (m *Manager) Restart(serverID int, by Actor) error {
	op, running := m.tryBeginOp(serverID, OpRestart, by, func(claimed bool) {
		if claimed && by != ActorSystem {
			logIntentError(serverID, database.RecordManualRestart(serverID))
		}
	})
	if op == nil {
		return &BusyError{Op: running.kind}
	}
	defer m.endOp(serverID, op)
	attempted, err := m.restartInner(op, serverID)
	// The start half has now run, or never will. Either way nothing is owed. If a
	// Stop overrode this restart it has already cleared this too.
	logIntentError(serverID, database.SetOwedStart(serverID, database.OwedNone))
	switch {
	case err == nil && attempted:
		recordEvent(serverID, database.EventRestarted, by, "")
	case err == nil:
		recordEvent(serverID, database.EventStopped, by, "restart cancelled")
	case attempted:
		recordEvent(serverID, database.EventStartFailed, by, err.Error())
	default:
		recordEvent(serverID, database.EventStopFailed, by, err.Error())
	}
	return err
}

// restartInner runs the two halves of a restart. attempted says whether the start
// half ran, which tells a failed start from a failed stop.
func (m *Manager) restartInner(op *serverOp, serverID int) (attempted bool, err error) {
	if err := m.stopInner(op, serverID); err != nil {
		return false, err
	}
	if op.skipStart.Load() {
		return false, nil
	}
	op.phase.Store(int32(phaseStarting))
	if err := op.ctx.Err(); err != nil {
		return true, errStartCancelled(err)
	}
	return true, m.startInner(op, serverID)
}

func (m *Manager) watchServer(serverID int, gen uint64) {
	const deadThreshold = 2

	interval := m.watchInterval
	if interval <= 0 {
		interval = defaultWatchInterval
	}

	consecutiveDead, unknownPolls := 0, 0
	for {
		time.Sleep(interval)

		m.mu.Lock()
		inst, tracked := m.instances[serverID]
		sameGen := tracked && inst.gen == gen
		var proc *managedProc
		var pid int
		var dockerMode bool
		state := instRunning
		if sameGen {
			proc = inst.proc
			pid = inst.pid
			dockerMode = inst.dockerMode
			state = inst.state
		}
		m.mu.Unlock()
		if !tracked || !sameGen {
			// Stop/Restart already replaced or removed this instance.
			return
		}
		if state != instRunning {
			// An operation owns this shutdown and removes the instance when done. Keep
			// watching rather than returning: a failed stop hands it back to us.
			consecutiveDead = 0
			m.broadcast()
			continue
		}

		srv, err := database.GetServer(serverID)
		if err != nil {
			return
		}

		procAlive := proc != nil && !proc.exited()
		if procAlive && !dockerMode {
			// A container is watched directly; its PID may not be visible here.
			procAlive = processRunning(pid)
		}
		if procAlive {
			consecutiveDead, unknownPolls = 0, 0
			m.broadcast()
			continue
		}

		// The process is gone. A busy port only keeps the server alive if the holder
		// is provably this server's, such as a child Arma left bound to the port.
		if !dockerMode {
			portSt, owner := inspectGamePort(srv)
			var verdict portVerdict
			verdict, unknownPolls = judgePort(portSt, unknownPolls)
			switch verdict {
			case verdictRetarget:
				m.retargetInstance(serverID, gen, owner)
				consecutiveDead = 0
				m.broadcast()
				continue
			case verdictHold:
				m.broadcast()
				continue
			}
			if portSt == portForeign || portSt == portOwnerUnknown {
				holder := describePortHolder(srv.GamePort)
				log.Printf("[SERVER] server %d exited and something else holds game port %d%s",
					serverID, srv.GamePort, holder)
				recordEvent(serverID, database.EventPortHeld, ActorSystem,
					fmt.Sprintf("game port %d is in use by something else%s", srv.GamePort, holder))
			}
		}

		consecutiveDead++
		if consecutiveDead < deadThreshold {
			continue
		}
		if m.claimExit(serverID, gen) {
			break
		}
		// A stop claimed it first; it's handling this now.
		consecutiveDead = 0
	}

	// Cleanup runs as an operation so it can't race a start's content pins.
	op, err := m.waitAndBeginOp(context.Background(), serverID, OpCleanup, ActorSystem)
	if err != nil {
		return
	}
	defer m.endOp(serverID, op)

	m.mu.Lock()
	inst, ok := m.instances[serverID]
	m.mu.Unlock()
	if ok && inst.gen == gen {
		m.finishExited(inst)
	}
}

// retargetInstance points an instance at the child process that took over from it.
func (m *Manager) retargetInstance(serverID int, gen uint64, pid int) {
	m.mu.Lock()
	inst, ok := m.instances[serverID]
	if !ok || inst.gen != gen || inst.state != instRunning {
		m.mu.Unlock()
		return
	}
	inst.pid = pid
	inst.proc = adoptProcess(pid)
	rpt := inst.rptPath
	m.mu.Unlock()
	_ = database.UpdateServerProcess(serverID, database.ServerStatusRunning, pid, captureCreateMs(pid), rpt)
	log.Printf("[SERVER] server %d continues as pid=%d, which now holds its game port", serverID, pid)
}

// finishExited cleans up after a process that exited on its own, then reports it.
// The caller holds the server's op.
func (m *Manager) finishExited(inst *runningInstance) {
	if inst.proc != nil {
		_ = inst.proc.wait(30 * time.Second)
	}
	if !m.removeInstance(inst) {
		return
	}
	m.stopHeadlessFor(inst.serverID, inst)

	activeRPT := inst.rptPath
	if srv, err := database.GetServer(inst.serverID); err == nil {
		activeRPT = srv.ActiveRPT
	}
	m.markStopped(inst.serverID, activeRPT)

	ev := ExitEvent{ServerID: inst.serverID, StartedAt: inst.startedAt, At: time.Now()}
	if inst.proc != nil {
		ev.ExitCode, ev.HasCode = inst.proc.exitCode()
	}
	detail := ""
	if ev.HasCode {
		detail = fmt.Sprintf("exit code %d", ev.ExitCode)
	}
	recordEvent(inst.serverID, database.EventExited, ActorSystem, detail)
	m.notifyExit(ev)
}
