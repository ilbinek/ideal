package automation

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/server"
)

// fakeOps stands in for the server manager, and runs guards like the real one does.
type fakeOps struct {
	mu        sync.Mutex
	starts    int
	restarts  int
	startErr  error
	running   map[int]bool
	startedAt map[int]time.Time
	messages  []string
	sayErr    error
}

func (o *fakeOps) StartSystem(id int, guard func() error) error {
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.starts++
	return o.startErr
}

func (o *fakeOps) RestartPlanned(id int, guard func() error) (bool, error) {
	if guard != nil {
		if err := guard(); err != nil {
			return false, err
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.restarts++
	return true, o.startErr
}

func (o *fakeOps) Running(id int) (bool, time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running[id], o.startedAt[id]
}

func (o *fakeOps) CurrentOp(int) server.OpKind { return "" }

func (o *fakeOps) SendRConMessage(id int, message string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.messages = append(o.messages, message)
	return o.sayErr
}

type harness struct {
	t     *testing.T
	c     *Coordinator
	ops   *fakeOps
	id    int
	clock time.Time

	mu       sync.Mutex
	notified []string
}

func newHarness(t *testing.T) *harness {
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
	id, err := database.CreateServer("Main", "main", branches[0].ID, nil, 42302, 42303, "Server", "")
	if err != nil {
		t.Fatal(err)
	}

	h := &harness{t: t, id: int(id), clock: t0, ops: &fakeOps{running: map[int]bool{}, startedAt: map[int]time.Time{}}}
	h.c = newCoordinator(h.ops, func() time.Time { return h.clock })
	h.c.spawn = func(fn func()) { fn() }
	h.c.notify = func(title, _, _ string) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.notified = append(h.notified, title)
	}
	h.c.startedAt = t0
	return h
}

func (h *harness) setToggles(autostart, restart bool) {
	h.t.Helper()
	if err := database.SaveServerAutomation(h.id, autostart, restart, false, []int{15, 5, 1}, nil, 0); err != nil {
		h.t.Fatal(err)
	}
}

// drain handles queued events, as the loop would between steps.
func (h *harness) drain() {
	h.t.Helper()
	for {
		select {
		case ev := <-h.c.events:
			h.c.handle(ev)
		default:
			return
		}
	}
}

func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.clock = h.clock.Add(d)
	h.c.step()
	h.drain()
}

func (h *harness) exit(startedAt time.Time) {
	h.t.Helper()
	h.c.handle(event{exit: &server.ExitEvent{ServerID: h.id, StartedAt: startedAt, At: h.clock}})
}

func (h *harness) automation() *database.ServerAutomation {
	h.t.Helper()
	a, err := database.GetServerAutomation(h.id)
	if err != nil {
		h.t.Fatal(err)
	}
	return a
}

func (h *harness) eventCount(kind string) int {
	h.t.Helper()
	events, err := database.ListServerEvents(h.id, 100, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func (h *harness) notifiedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.notified)
}

func TestExitRestartsAfterBackoff(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))

	h.advance(9 * time.Second)
	if h.ops.starts != 0 {
		t.Fatal("restarted before the backoff ended")
	}
	h.advance(time.Second)
	if h.ops.starts != 1 {
		t.Fatalf("starts %d", h.ops.starts)
	}
	if a := h.automation(); a.OwedStart != database.OwedNone {
		t.Fatalf("owed after success: %q", a.OwedStart)
	}
	if n := h.eventCount(database.EventRestarted); n != 1 {
		t.Fatalf("restarted events %d", n)
	}
	if h.notifiedCount() != 1 {
		t.Fatalf("notifications %v", h.notified)
	}
}

// The user's rule: with the toggle off, never restart automatically, in any case.
func TestToggleOffNeverRestarts(t *testing.T) {
	h := newHarness(t)
	h.setToggles(true, false) // launch toggle on must not change this
	h.exit(t0.Add(-time.Minute))
	for i := 0; i < 5; i++ {
		h.advance(10 * time.Minute)
	}
	if h.ops.starts != 0 {
		t.Fatalf("restarted %d time(s) with the toggle off", h.ops.starts)
	}
	if a := h.automation(); a.OwedStart != database.OwedNone || len(a.CrashHistory) != 0 {
		t.Fatalf("state %+v", a)
	}
	if h.notifiedCount() != 1 {
		t.Fatalf("the exit should still be reported: %v", h.notified)
	}
}

func TestToggleOffDuringBackoffCancels(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))
	h.setToggles(false, false)
	h.advance(time.Minute)
	if h.ops.starts != 0 {
		t.Fatal("restarted after the toggle was turned off")
	}
	if a := h.automation(); a.OwedStart != database.OwedNone {
		t.Fatalf("owed %q", a.OwedStart)
	}
}

func TestManualStopDuringBackoffCancels(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))
	if err := database.RecordManualStop(h.id); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Minute)
	if h.ops.starts != 0 {
		t.Fatal("restarted a server someone stopped")
	}
}

func TestStopLandingAsRetryFiresIsSkipped(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))

	// The Stop lands after the retry's already been decided: the guard catches it.
	guard := retryGuard(h.id)
	if err := database.RecordManualStop(h.id); err != nil {
		t.Fatal(err)
	}
	if err := guard(); !errors.Is(err, errSkipped) {
		t.Fatalf("guard: %v", err)
	}
}

func TestStopRacingExitDoesNotRetry(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	if err := database.RecordManualStop(h.id); err != nil {
		t.Fatal(err)
	}
	h.exit(t0.Add(-time.Minute))
	h.advance(time.Minute)
	if h.ops.starts != 0 || h.notifiedCount() != 0 {
		t.Fatalf("starts %d, notifications %v", h.ops.starts, h.notified)
	}
}

func TestCrashLoopGivesUpOnce(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.ops.startErr = errors.New("mod is broken")

	h.exit(t0.Add(-time.Second))
	for i := 0; i < 20; i++ {
		h.advance(5 * time.Minute / 4)
	}
	a := h.automation()
	if a.GaveUpAt == 0 {
		t.Fatalf("never gave up: %+v (starts %d)", a, h.ops.starts)
	}
	if h.ops.starts != maxFailures-1 {
		t.Fatalf("attempts %d", h.ops.starts)
	}
	if n := h.eventCount(database.EventGaveUp); n != 1 {
		t.Fatalf("give-up events %d", n)
	}
	last := h.notified[len(h.notified)-1]
	if last != "Gave up restarting server" {
		t.Fatalf("notifications %v", h.notified)
	}

	// A further exit report changes nothing until someone steps in.
	starts := h.ops.starts
	h.advance(time.Hour)
	if h.ops.starts != starts {
		t.Fatal("kept trying after giving up")
	}

	// Someone starts it by hand, and its next exit gets a fresh set of retries.
	if err := database.RecordManualStart(h.id); err != nil {
		t.Fatal(err)
	}
	h.ops.startErr = nil
	h.exit(h.clock.Add(-time.Minute))
	h.advance(10 * time.Second)
	if h.ops.starts != starts+1 {
		t.Fatalf("starts %d, want %d", h.ops.starts, starts+1)
	}
}

func TestAlreadyRunningIsNotAFailure(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.ops.startErr = server.ErrAlreadyRunning
	h.exit(t0.Add(-time.Minute))
	h.advance(10 * time.Second)
	a := h.automation()
	if len(a.CrashHistory) != 1 || a.OwedStart != database.OwedNone {
		t.Fatalf("someone starting it first must not count as a failure: %+v", a)
	}
	if h.eventCount(database.EventStartFailed) != 0 {
		t.Fatal("recorded a failure")
	}
}

func TestPauseCancelsRetryWithoutCatchUp(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))
	settings := database.AutomationSettings{Paused: true, BootDelaySec: 0, BootGapSec: 0, WarningTemplate: "x"}
	if err := database.SaveAutomationSettings(settings); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Minute)
	if h.ops.starts != 0 {
		t.Fatal("restarted while paused")
	}
	settings.Paused = false
	if err := database.SaveAutomationSettings(settings); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Hour)
	if h.ops.starts != 0 {
		t.Fatal("caught up after unpausing")
	}
	if a := h.automation(); a.OwedStart != database.OwedNone {
		t.Fatalf("owed %q", a.OwedStart)
	}
}

func TestExitWhilePausedStillNotifies(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	if err := database.SaveAutomationSettings(database.AutomationSettings{Paused: true, WarningTemplate: "x"}); err != nil {
		t.Fatal(err)
	}
	h.exit(t0.Add(-time.Minute))
	if h.notifiedCount() != 1 {
		t.Fatalf("notifications %v", h.notified)
	}
}

func TestBootQueueStaggers(t *testing.T) {
	h := newHarness(t)
	h.setToggles(true, false)
	branches, _ := database.GetBranches()
	id2, err := database.CreateServer("Second", "second", branches[0].ID, nil, 42402, 42403, "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SaveServerAutomation(int(id2), true, false, false, []int{5}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveAutomationSettings(database.AutomationSettings{BootDelaySec: 30, BootGapSec: 20, WarningTemplate: "x"}); err != nil {
		t.Fatal(err)
	}

	h.c.planBoot()
	h.advance(29 * time.Second)
	if h.ops.starts != 0 {
		t.Fatal("started during the launch delay")
	}
	h.advance(time.Second)
	if h.ops.starts != 1 {
		t.Fatalf("starts after delay: %d", h.ops.starts)
	}
	h.advance(19 * time.Second)
	if h.ops.starts != 1 {
		t.Fatal("second server didn't wait out the gap")
	}
	h.advance(time.Second)
	if h.ops.starts != 2 {
		t.Fatalf("starts after gap: %d", h.ops.starts)
	}
	if n := h.eventCount(database.EventBootStart); n != 1 {
		// Counted for the first server only; the second has its own history.
		t.Fatalf("boot events %d", n)
	}
}

func TestBootStartFailureNotifiesAndStaysDownWhenToggleOff(t *testing.T) {
	h := newHarness(t)
	h.setToggles(true, false)
	if err := database.SaveAutomationSettings(database.AutomationSettings{WarningTemplate: "x"}); err != nil {
		t.Fatal(err)
	}
	h.ops.startErr = errors.New("branch is not installed")
	h.c.planBoot()
	h.advance(time.Second)
	if h.ops.starts != 1 || h.notifiedCount() != 1 {
		t.Fatalf("starts %d, notifications %v", h.ops.starts, h.notified)
	}
	h.advance(time.Hour)
	if h.ops.starts != 1 {
		t.Fatalf("retried with crash restart off: %d", h.ops.starts)
	}
}

func TestBootStartFailureRetriesWhenToggleOn(t *testing.T) {
	h := newHarness(t)
	h.setToggles(true, true)
	if err := database.SaveAutomationSettings(database.AutomationSettings{WarningTemplate: "x"}); err != nil {
		t.Fatal(err)
	}
	h.ops.startErr = errors.New("port busy")
	h.c.planBoot()
	h.advance(time.Second)
	h.ops.startErr = nil
	h.advance(10 * time.Second)
	if h.ops.starts != 2 {
		t.Fatalf("starts %d", h.ops.starts)
	}
}

func TestStatusViewReported(t *testing.T) {
	h := newHarness(t)
	h.setToggles(false, true)
	h.exit(t0.Add(-time.Minute))
	h.c.step()
	snap := server.StatusSnapshot{ID: h.id}
	h.c.decorate(&snap)
	if snap.RetryAt != t0.Add(10*time.Second).Unix() || snap.Failures != 1 {
		t.Fatalf("%+v", snap)
	}
}

func TestDiscordBlockedDoesNotDelayRestart(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var sends atomic.Int32
	notifier := newDiscordNotifier(func(notification) error {
		sends.Add(1)
		<-release
		return nil
	})

	h := newHarness(t)
	h.c.notify = notifier.enqueue
	h.setToggles(false, true)

	// The webhook hangs and the queue fills up, and yet nothing here may block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			notifier.enqueue("x", "y", "z")
		}
		h.exit(t0.Add(-time.Minute))
		h.advance(10 * time.Second)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hanging webhook blocked the restart path")
	}
	if h.ops.starts != 1 {
		t.Fatalf("starts %d", h.ops.starts)
	}
}
