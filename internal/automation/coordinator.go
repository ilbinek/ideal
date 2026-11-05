package automation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/discordhook"
	"arma3modmanager/internal/server"
)

// errSkipped means an action's guard found it no longer applies. It isn't a failure.
var errSkipped = errors.New("no longer applies")

// bootRetryLimit bounds how long a server finishing an interrupted stop can hold up
// its boot start. The stop chain's longest path is well inside this.
const bootRetryLimit = 5 * time.Minute

// serverOps is what the coordinator needs from the server manager.
type serverOps interface {
	StartSystem(serverID int, guard func() error) error
	RestartPlanned(serverID int, guard func() error) (attempted bool, err error)
	Running(serverID int) (running bool, startedAt time.Time)
	CurrentOp(serverID int) server.OpKind
	SendRConMessage(serverID int, message string) error
}

type startSource int

const (
	sourceRetry startSource = iota
	sourceBoot
	sourcePlanned
)

type event struct {
	exit    *server.ExitEvent
	result  *actionResult
	wake    bool
	warning *warningResult
}

type actionResult struct {
	serverID  int
	source    startSource
	owed      string
	attempted bool // planned restarts only: the start half ran
	err       error
}

type warningResult struct {
	serverID int
	minutes  int
	err      error
}

// statusView is what status snapshots show. Replaced whole, read without locks.
type statusView struct {
	paused  bool
	servers map[int]serverStatus
}

type serverStatus struct {
	autostart     bool
	retryAt       int64
	failures      int
	gaveUp        bool
	userStopped   bool
	nextRestartAt int64
}

// Coordinator carries out automation. One goroutine owns its state; anything slow
// runs in a short-lived action goroutine that reports back as an event.
type Coordinator struct {
	ops    serverOps
	now    func() time.Time
	spawn  func(func())
	notify func(title, serverName, detail string)

	events  chan event
	started atomic.Bool
	view    atomic.Pointer[statusView]

	// Owned by the loop goroutine.
	startedAt  time.Time
	retries    map[int]time.Time
	bootQueue  []BootItem
	nextBootAt time.Time
	inflight   map[int]bool
	warned     map[warnKey]bool
}

// Default is the process-wide coordinator.
var Default = newCoordinator(server.Default, time.Now)

func newCoordinator(ops serverOps, now func() time.Time) *Coordinator {
	notifier := newDiscordNotifier(sendDiscord)
	return &Coordinator{
		ops:      ops,
		now:      now,
		spawn:    func(fn func()) { go fn() },
		notify:   notifier.enqueue,
		events:   make(chan event, 256),
		retries:  make(map[int]time.Time),
		inflight: make(map[int]bool),
		warned:   make(map[warnKey]bool),
	}
}

// Start begins automation. Call once, after server.Init has settled statuses and
// re-adopted whatever is still running.
func (c *Coordinator) Start() {
	if !c.started.CompareAndSwap(false, true) {
		return
	}
	server.Default.SetExitObserver(func(ev server.ExitEvent) {
		c.post(event{exit: &ev})
	})
	server.Default.SetStatusDecorator(c.decorate)
	go c.loop()
}

// Reload tells the coordinator settings changed, so it acts on them right away
// rather than at its next tick. It never blocks.
func (c *Coordinator) Reload() {
	if c.started.Load() {
		c.post(event{wake: true})
	}
}

func (c *Coordinator) post(ev event) {
	select {
	case c.events <- ev:
	default:
		// Full: deliver without blocking the caller, since losing an exit would
		// leave a server down.
		go func() { c.events <- ev }()
	}
}

func (c *Coordinator) loop() {
	c.planBoot()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case ev := <-c.events:
			c.handle(ev)
		case <-ticker.C:
		}
		c.step()
	}
}

func (c *Coordinator) handle(ev event) {
	switch {
	case ev.exit != nil:
		c.onExit(*ev.exit)
	case ev.result != nil:
		c.onResult(*ev.result)
	case ev.warning != nil:
		c.onWarningResult(*ev.warning)
	}
}

// planBoot works out which servers to start now that Ideal has launched.
func (c *Coordinator) planBoot() {
	c.startedAt = c.now()
	servers, err := database.GetServers()
	if err != nil {
		log.Printf("[AUTOMATION] cannot plan launch starts: %v", err)
		return
	}
	settings, err := database.GetAutomationSettings()
	if err != nil {
		log.Printf("[AUTOMATION] cannot plan launch starts: %v", err)
		return
	}
	all, err := database.GetAllServerAutomation()
	if err != nil {
		log.Printf("[AUTOMATION] cannot plan launch starts: %v", err)
		return
	}
	views := make([]ServerView, 0, len(servers))
	for i := range servers {
		a := all[servers[i].ID]
		if a == nil {
			continue // no row means everything is off
		}
		views = append(views, ServerView{
			ID:         servers[i].ID,
			Up:         c.serverIsUp(&servers[i]),
			Automation: a,
		})
	}
	decision := BootPlan(views, settings.Paused)
	for _, id := range decision.ClearOwed {
		if err := database.SetOwedStart(id, database.OwedNone); err != nil {
			log.Printf("[AUTOMATION] clearing pending start for server %d: %v", id, err)
		}
	}
	c.bootQueue = decision.Start
	c.nextBootAt = c.startedAt.Add(time.Duration(settings.BootDelaySec) * time.Second)
	if len(c.bootQueue) > 0 {
		log.Printf("[AUTOMATION] %d server(s) to start after launch", len(c.bootQueue))
	}
}

// serverIsUp reports whether a server has nothing to start. A server whose stop is
// being finished in the background counts as down, as it soon will be. A live process
// Ideal couldn't verify counts as up, because starting would only collide with it.
func (c *Coordinator) serverIsUp(s *database.Server) bool {
	if resuming, ok := c.ops.(interface{ ResumingStop(int) bool }); ok && resuming.ResumingStop(s.ID) {
		return false
	}
	if running, _ := c.ops.Running(s.ID); running {
		return true
	}
	return s.Status == database.ServerStatusUnknown && s.PID > 0
}

// step does whatever has come due. It's cheap enough to run every second: settings
// are re-read each time, so what's in the database always wins over what's in memory.
func (c *Coordinator) step() {
	now := c.now()
	settings, err := database.GetAutomationSettings()
	if err != nil {
		log.Printf("[AUTOMATION] reading settings: %v", err)
		return
	}
	all, err := database.GetAllServerAutomation()
	if err != nil {
		log.Printf("[AUTOMATION] reading server settings: %v", err)
		return
	}

	if settings.Paused {
		c.dropPending(all)
	}
	c.pruneRetries(all)
	c.fireRetries(now)
	c.fireBoot(now, settings)
	c.stepSchedules(now, settings, all)
	c.publishView(now, settings, all)
}

// dropPending cancels everything owed while paused. There's no catch-up on unpausing.
func (c *Coordinator) dropPending(all map[int]*database.ServerAutomation) {
	for _, item := range c.bootQueue {
		_ = database.SetOwedStart(item.ServerID, database.OwedNone)
	}
	c.bootQueue = nil
	for id := range c.retries {
		_ = database.SetOwedStart(id, database.OwedNone)
		delete(c.retries, id)
	}
	for id, a := range all {
		if a.OwedStart != database.OwedNone && !c.inflight[id] {
			_ = database.SetOwedStart(id, database.OwedNone)
		}
	}
}

// pruneRetries drops retries that no longer apply: someone started or stopped the
// server, turned the toggle off, or deleted it. Each of those clears the owed start.
func (c *Coordinator) pruneRetries(all map[int]*database.ServerAutomation) {
	for id := range c.retries {
		a := all[id]
		if a == nil || a.OwedStart != database.OwedCrash || !a.RestartOnCrash || a.UserStopped {
			if a != nil && a.OwedStart == database.OwedCrash {
				_ = database.SetOwedStart(id, database.OwedNone)
			}
			delete(c.retries, id)
		}
	}
}

func (c *Coordinator) fireRetries(now time.Time) {
	for id, due := range c.retries {
		if now.Before(due) || c.inflight[id] {
			continue
		}
		delete(c.retries, id)
		c.runStart(id, sourceRetry, database.OwedCrash, retryGuard(id))
	}
}

func (c *Coordinator) fireBoot(now time.Time, settings *database.AutomationSettings) {
	if len(c.bootQueue) == 0 || now.Before(c.nextBootAt) {
		return
	}
	item := c.bootQueue[0]
	if c.inflight[item.ServerID] {
		return
	}
	c.bootQueue = c.bootQueue[1:]
	c.nextBootAt = now.Add(time.Duration(settings.BootGapSec) * time.Second)
	c.runStart(item.ServerID, sourceBoot, item.Owed, bootGuard(item))
}

// runStart starts a server in the background. The guard runs once the server's
// operation lock is held, closing the race with a manual Stop.
func (c *Coordinator) runStart(serverID int, source startSource, owed string, guard func() error) {
	c.inflight[serverID] = true
	c.spawn(func() {
		err := c.ops.StartSystem(serverID, guard)
		c.post(event{result: &actionResult{serverID: serverID, source: source, owed: owed, attempted: true, err: err}})
	})
}

func retryGuard(serverID int) func() error {
	return func() error {
		a, paused, err := currentIntent(serverID)
		if err != nil {
			return err
		}
		if paused || a.UserStopped || !a.RestartOnCrash || a.OwedStart != database.OwedCrash {
			return errSkipped
		}
		return nil
	}
}

func bootGuard(item BootItem) func() error {
	return func() error {
		a, paused, err := currentIntent(item.ServerID)
		if err != nil {
			return err
		}
		if paused || a.UserStopped || a.GaveUpAt > 0 {
			return errSkipped
		}
		if item.Owed == database.OwedNone && !a.Autostart {
			return errSkipped // toggled off while waiting in the queue
		}
		return nil
	}
}

func currentIntent(serverID int) (*database.ServerAutomation, bool, error) {
	a, err := database.GetServerAutomation(serverID)
	if err != nil {
		return nil, false, err
	}
	settings, err := database.GetAutomationSettings()
	if err != nil {
		return nil, false, err
	}
	return a, settings.Paused, nil
}

// onExit reacts to a server exiting without anyone asking. The history entry is
// already written; this decides whether to bring it back.
func (c *Coordinator) onExit(ev server.ExitEvent) {
	a, paused, err := currentIntent(ev.ServerID)
	if err != nil {
		log.Printf("[AUTOMATION] handling exit of server %d: %v", ev.ServerID, err)
		return
	}
	const title = "Server exited unexpectedly"
	switch {
	case a.UserStopped:
		// Someone asked for a stop just as it died, so there's nothing to report.
	case paused:
		c.notify(title, serverName(ev.ServerID), "Ideal will not restart it: automation is paused.")
	case !a.RestartOnCrash:
		c.notify(title, serverName(ev.ServerID), "Ideal will not restart it: restarting after an exit is off for this server.")
	default:
		c.recordFailure(a, ev.At, ev.StartedAt, title)
	}
}

// recordFailure counts a failed run towards giving up, and schedules the retry or
// announces the give-up. title, if set, announces the failure itself.
func (c *Coordinator) recordFailure(a *database.ServerAutomation, now, instStart time.Time, title string) {
	history, delay, giveUp := OnFailure(unixTimes(a.CrashHistory), now, instStart)
	name := serverName(a.ServerID)
	if giveUp {
		delete(c.retries, a.ServerID)
		if err := database.SetCrashState(a.ServerID, toUnix(history), database.OwedNone, now.Unix()); err != nil {
			log.Printf("[AUTOMATION] saving give-up for server %d: %v", a.ServerID, err)
		}
		detail := fmt.Sprintf("%d failures within %s. It stays off until someone starts it.", len(history), failureWindow)
		addEvent(a.ServerID, database.EventGaveUp, detail)
		c.notify("Gave up restarting server", name, detail)
		return
	}
	// Persisted so that a person's Stop during the wait has an owed start to clear.
	if err := database.SetCrashState(a.ServerID, toUnix(history), database.OwedCrash, 0); err != nil {
		log.Printf("[AUTOMATION] saving failure for server %d: %v", a.ServerID, err)
	}
	c.retries[a.ServerID] = now.Add(delay)
	detail := fmt.Sprintf("Trying again in %s (failure %d of %d).", delay, len(history), maxFailures)
	addEvent(a.ServerID, database.EventRetryScheduled, detail)
	if title != "" {
		c.notify(title, name, detail)
	}
}

func (c *Coordinator) onResult(r actionResult) {
	delete(c.inflight, r.serverID)
	a, paused, err := currentIntent(r.serverID)
	if err != nil {
		log.Printf("[AUTOMATION] handling result for server %d: %v", r.serverID, err)
		return
	}

	if c.onPlannedStopHalf(r, a) {
		return
	}

	var busy *server.BusyError
	switch {
	case r.err == nil:
		_ = database.SetOwedStart(r.serverID, database.OwedNone)
		switch r.source {
		case sourceBoot:
			addEvent(r.serverID, database.EventBootStart, bootDetail(r.owed))
		case sourceRetry:
			addEvent(r.serverID, database.EventRestarted, "after exiting unexpectedly")
		case sourcePlanned:
			addEvent(r.serverID, database.EventPlannedRestart, "")
		}
		return

	case errors.As(r.err, &busy):
		// A boot start can catch the end of a stop being finished in the background.
		if r.source == sourceBoot && c.now().Sub(c.startedAt) < bootRetryLimit {
			c.bootQueue = append(c.bootQueue, BootItem{ServerID: r.serverID, Owed: r.owed})
			return
		}
		// Otherwise someone else is acting on this server, and their intent stands.
		if a.OwedStart == r.owed {
			_ = database.SetOwedStart(r.serverID, database.OwedNone)
		}
		return

	case errors.Is(r.err, errSkipped), errors.Is(r.err, server.ErrAlreadyRunning):
		if a.OwedStart == r.owed {
			_ = database.SetOwedStart(r.serverID, database.OwedNone)
		}
		return
	}

	// A real failure.
	addEvent(r.serverID, database.EventStartFailed, r.err.Error())
	name := serverName(r.serverID)
	switch r.source {
	case sourceBoot:
		c.notify("Server failed to start on launch", name, r.err.Error())
	case sourcePlanned:
		c.notify("Server failed to come back from a planned restart", name, r.err.Error())
	}
	if a.RestartOnCrash && !a.UserStopped && !paused {
		c.recordFailure(a, c.now(), time.Time{}, "")
		return
	}
	_ = database.SetOwedStart(r.serverID, database.OwedNone)
}

func bootDetail(owed string) string {
	if owed != database.OwedNone {
		return "finishing a restart that Ideal shutting down interrupted"
	}
	return ""
}

func addEvent(serverID int, kind, detail string) {
	if err := database.AddServerEvent(serverID, kind, string(server.ActorSystem), detail); err != nil {
		log.Printf("[AUTOMATION] recording %s for server %d: %v", kind, serverID, err)
	}
}

func serverName(serverID int) string {
	if srv, err := database.GetServer(serverID); err == nil {
		return srv.Name
	}
	return fmt.Sprintf("Server %d", serverID)
}

func (c *Coordinator) publishView(now time.Time, settings *database.AutomationSettings, all map[int]*database.ServerAutomation) {
	view := &statusView{paused: settings.Paused, servers: make(map[int]serverStatus, len(all))}
	for id, a := range all {
		st := serverStatus{
			autostart:     a.Autostart,
			failures:      len(a.CrashHistory),
			gaveUp:        a.GaveUpAt > 0,
			userStopped:   a.UserStopped,
			nextRestartAt: nextRestartFor(now, a),
		}
		if due, ok := c.retries[id]; ok {
			st.retryAt = due.Unix()
		}
		view.servers[id] = st
	}
	previous := c.view.Swap(view)
	if !sameView(previous, view) {
		server.Default.Broadcast()
	}
}

func sameView(a, b *statusView) bool {
	if a == nil || b == nil || a.paused != b.paused || len(a.servers) != len(b.servers) {
		return false
	}
	for id, st := range a.servers {
		if b.servers[id] != st {
			return false
		}
	}
	return true
}

// decorate adds automation state to a status snapshot. It's called with the
// manager's lock held, so it must not take locks of its own.
func (c *Coordinator) decorate(snap *server.StatusSnapshot) {
	view := c.view.Load()
	if view == nil {
		return
	}
	snap.Paused = view.paused
	st, ok := view.servers[snap.ID]
	if !ok {
		return
	}
	snap.Autostart = st.autostart
	snap.RetryAt = st.retryAt
	snap.Failures = st.failures
	snap.GaveUp = st.gaveUp
	snap.UserStopped = st.userStopped
	snap.NextRestartAt = st.nextRestartAt
}

// discordNotifier sends notifications from one background worker, so a slow webhook
// can't hold up a restart. When the queue is full, it drops and logs.
type discordNotifier struct {
	queue chan notification
	once  sync.Once
	send  func(notification) error
}

type notification struct {
	title, server, detail string
}

func newDiscordNotifier(send func(notification) error) *discordNotifier {
	return &discordNotifier{queue: make(chan notification, 32), send: send}
}

func (n *discordNotifier) enqueue(title, serverName, detail string) {
	n.once.Do(func() { go n.run() })
	select {
	case n.queue <- notification{title: title, server: serverName, detail: detail}:
	default:
		log.Printf("[AUTOMATION] notification queue full, dropping: %s (%s)", title, serverName)
	}
}

func (n *discordNotifier) run() {
	for item := range n.queue {
		if err := n.send(item); err != nil {
			log.Printf("[AUTOMATION] notification failed: %v", err)
		}
	}
}

func sendDiscord(item notification) error {
	cfg, err := database.GetConfig()
	if err != nil || cfg == nil {
		return nil // no settings saved yet, so no webhook
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return discordhook.NotifyServerEvent(ctx, cfg.DiscordWebhookURL, item.title, item.server, item.detail)
}
