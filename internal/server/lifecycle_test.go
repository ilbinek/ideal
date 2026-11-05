package server

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"arma3modmanager/internal/database"
)

const testWatchInterval = 10 * time.Millisecond

// lifecycleFixture is a server row plus a Manager with fake process handling.
type lifecycleFixture struct {
	m     *Manager
	id    int
	exits atomic.Int32
	mu    sync.Mutex
	proc  chan struct{} // closes when the current fake process "exits"
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	t.Setenv("IDEAL_DATA_DIR", t.TempDir())
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	branches, err := database.GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	game, steam := freePort(t), freePort(t)
	id64, err := database.CreateServer("Main", "main", branches[0].ID, nil, game, steam, "Server", "")
	if err != nil {
		t.Fatal(err)
	}

	f := &lifecycleFixture{
		m: &Manager{
			instances:     make(map[int]*runningInstance),
			headless:      make(map[hcKey]*runningHeadless),
			subs:          make(map[chan AllStatusSnapshot]struct{}),
			watchInterval: testWatchInterval,
		},
		id: int(id64),
	}
	f.m.SetExitObserver(func(ExitEvent) { f.exits.Add(1) })
	f.m.launcher = f.launchOK
	f.m.terminator = func(context.Context, *runningInstance, *serverOp) error {
		f.exitProcess()
		return nil
	}
	f.m.headlessStarter = func(context.Context, int, int) error { return nil }
	return f
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// launchOK pretends to spawn a process. It uses this test's own PID so liveness
// checks see something real until the fake exits.
func (f *lifecycleFixture) launchOK(context.Context, int) (*launchedServer, error) {
	done := make(chan struct{})
	f.mu.Lock()
	f.proc = done
	f.mu.Unlock()
	return &launchedServer{
		proc:      &managedProc{pid: os.Getpid(), done: done},
		startTime: time.Now(),
	}, nil
}

func (f *lifecycleFixture) exitProcess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.proc != nil {
		close(f.proc)
		f.proc = nil
	}
}

func (f *lifecycleFixture) status(t *testing.T) string {
	t.Helper()
	srv, err := database.GetServer(f.id)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Status
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *lifecycleFixture) tracked() bool {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	_, ok := f.m.instances[f.id]
	return ok
}

func TestStopDuringStartPreempts(t *testing.T) {
	f := newLifecycleFixture(t)
	entered := make(chan struct{})
	f.m.launcher = func(ctx context.Context, _ int) (*launchedServer, error) {
		close(entered)
		<-ctx.Done()
		return nil, errStartCancelled(ctx.Err())
	}

	startErr := make(chan error, 1)
	go func() { startErr <- f.m.Start(f.id, "alice") }()
	<-entered

	if err := f.m.Stop(f.id, "bob"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-startErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("start should be cancelled, got %v", err)
	}
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
	if f.tracked() {
		t.Fatal("instance registered despite cancelled start")
	}
}

func TestConcurrentStartBusy(t *testing.T) {
	f := newLifecycleFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.m.launcher = func(ctx context.Context, id int) (*launchedServer, error) {
		close(entered)
		<-release
		return f.launchOK(ctx, id)
	}

	first := make(chan error, 1)
	go func() { first <- f.m.Start(f.id, "alice") }()
	<-entered

	var busy *BusyError
	if err := f.m.Start(f.id, "bob"); !errors.As(err, &busy) || busy.Op != OpStart {
		t.Fatalf("second start: %v", err)
	}
	if err := f.m.Restart(f.id, "bob"); !errors.As(err, &busy) {
		t.Fatalf("restart during start: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := f.m.Start(f.id, "bob"); err == nil {
		t.Fatal("start while running should fail")
	}
	if err := f.m.Stop(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
}

// After #shutdown the process exits by itself while the stop is still waiting.
// That must not read as a crash, however many watcher polls go by.
func TestExitDuringStopNotCrash(t *testing.T) {
	f := newLifecycleFixture(t)
	f.m.terminator = func(context.Context, *runningInstance, *serverOp) error {
		f.exitProcess()
		time.Sleep(10 * testWatchInterval)
		return nil
	}
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Stop(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * testWatchInterval)
	if n := f.exits.Load(); n != 0 {
		t.Fatalf("requested stop reported as %d unexpected exit(s)", n)
	}
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
}

func TestUnrequestedExitNotifiesOnce(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	f.exitProcess()
	waitFor(t, "exit event", func() bool { return f.exits.Load() > 0 })
	time.Sleep(5 * testWatchInterval)
	if n := f.exits.Load(); n != 1 {
		t.Fatalf("got %d exit events", n)
	}
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
	if f.tracked() {
		t.Fatal("instance still tracked")
	}
	if op := f.m.CurrentOp(f.id); op != "" {
		t.Fatalf("op %q left behind", op)
	}
}

// The process died, the watcher claimed it, and only then did a Stop arrive.
func TestStopLosesClaimRecordsCrash(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	// Stand in for the watcher having just claimed the exit, so this test alone
	// drives what happens next.
	f.m.mu.Lock()
	inst := f.m.instances[f.id]
	inst.state = instExited
	f.m.mu.Unlock()
	f.exitProcess()

	f.m.terminator = func(context.Context, *runningInstance, *serverOp) error {
		t.Error("nothing should be terminated: the process is already gone")
		return nil
	}
	if err := f.m.Stop(f.id, "bob"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if n := f.exits.Load(); n != 1 {
		t.Fatalf("got %d exit events", n)
	}
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
}

func TestFailedStopHandsBackToWatcher(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	f.m.terminator = func(context.Context, *runningInstance, *serverOp) error {
		return errors.New("refused")
	}
	if err := f.m.Stop(f.id, "alice"); err == nil {
		t.Fatal("stop should report the failure while the process lives")
	}
	if got := f.status(t); got != database.ServerStatusRunning {
		t.Fatalf("status %q", got)
	}
	// The watcher must still be on duty.
	f.exitProcess()
	waitFor(t, "exit after failed stop", func() bool { return f.exits.Load() == 1 })
}

func TestStopDuringRestartSkipsStart(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	inTerminate, release := make(chan struct{}), make(chan struct{})
	f.m.terminator = func(context.Context, *runningInstance, *serverOp) error {
		close(inTerminate)
		<-release
		f.exitProcess()
		return nil
	}
	var launches atomic.Int32
	f.m.launcher = func(ctx context.Context, id int) (*launchedServer, error) {
		launches.Add(1)
		return f.launchOK(ctx, id)
	}

	restartErr := make(chan error, 1)
	go func() { restartErr <- f.m.Restart(f.id, "alice") }()
	<-inTerminate

	stopErr := make(chan error, 1)
	go func() { stopErr <- f.m.Stop(f.id, "bob") }()
	// Let the Stop register its wish before the restart's stop half finishes.
	waitFor(t, "skipStart", func() bool {
		f.m.opMu.Lock()
		defer f.m.opMu.Unlock()
		op, ok := f.m.ops[f.id]
		return ok && op.skipStart.Load()
	})
	close(release)

	if err := <-restartErr; err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := <-stopErr; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if n := launches.Load(); n != 0 {
		t.Fatalf("server started %d time(s) despite the stop", n)
	}
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
}

func TestForceReachesRunningStop(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	inTerminate := make(chan struct{})
	var sawForce atomic.Bool
	f.m.terminator = func(ctx context.Context, _ *runningInstance, op *serverOp) error {
		close(inTerminate)
		<-ctx.Done()
		sawForce.Store(op.forced.Load())
		f.exitProcess()
		return nil
	}

	stopErr := make(chan error, 1)
	go func() { stopErr <- f.m.Stop(f.id, "alice") }()
	<-inTerminate
	if err := f.m.ForceStop(f.id, "bob"); err != nil {
		t.Fatalf("force: %v", err)
	}
	if err := <-stopErr; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !sawForce.Load() {
		t.Fatal("the running stop never saw the force request")
	}
}

func TestNoHCAfterStop(t *testing.T) {
	f := newLifecycleFixture(t)
	if _, err := database.SyncHeadlessSlots(f.id, 2, "Main", "main"); err != nil {
		t.Fatal(err)
	}

	var started, cancelled atomic.Int32
	entered := make(chan struct{}, 2)
	f.m.headlessStarter = func(ctx context.Context, _, _ int) error {
		started.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		cancelled.Add(1)
		return ctx.Err()
	}

	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered: // auto-start is mid-client
	case <-time.After(5 * time.Second):
		t.Fatal("headless auto-start never ran")
	}
	if err := f.m.Stop(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	// Stop waits for the auto-start, so this holds as soon as it returns: the first
	// client was cancelled and the loop didn't go on to the second.
	if s, c := started.Load(), cancelled.Load(); s != 1 || c != 1 {
		t.Fatalf("started=%d cancelled=%d", s, c)
	}
	if _, ok := f.m.parentGeneration(f.id); ok {
		t.Fatal("parent should no longer accept headless clients")
	}
}

func TestParentGenerationRefusedOnceStopClaims(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.m.parentGeneration(f.id); !ok {
		t.Fatal("running server should accept headless clients")
	}
	if inst, exited := f.m.claimStop(f.id); inst == nil || exited {
		t.Fatalf("claim: %v %v", inst, exited)
	}
	if _, ok := f.m.parentGeneration(f.id); ok {
		t.Fatal("a stop has claimed it: no new headless clients")
	}
	f.m.mu.Lock()
	f.m.instances[f.id].state = instRunning
	f.m.mu.Unlock()
	if err := f.m.Stop(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestExclusiveBlocksLifecycle(t *testing.T) {
	f := newLifecycleFixture(t)
	end, err := f.m.BeginExclusive(f.id, OpDelete, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var busy *BusyError
	if err := f.m.Start(f.id, "bob"); !errors.As(err, &busy) || busy.Op != OpDelete {
		t.Fatalf("start: %v", err)
	}
	if err := f.m.Stop(f.id, "bob"); !errors.As(err, &busy) {
		t.Fatalf("stop: %v", err)
	}
	end()
	if err := f.m.Start(f.id, "bob"); err != nil {
		t.Fatalf("start after release: %v", err)
	}
	if err := f.m.Stop(f.id, "bob"); err != nil {
		t.Fatal(err)
	}
}

// Another program holding the game port used to read as the server still running,
// forever. It must count as the exit it is.
func TestForeignPortHolderDoesNotKeepServerRunning(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.m.Start(f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	srv, err := database.GetServer(f.id)
	if err != nil {
		t.Fatal(err)
	}
	// This test binary isn't an Arma server, so it stands in for the foreign holder.
	holder, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: srv.GamePort})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	f.exitProcess()
	waitFor(t, "exit despite foreign port holder", func() bool { return f.exits.Load() == 1 })
	if got := f.status(t); got != database.ServerStatusStopped {
		t.Fatalf("status %q", got)
	}
}
