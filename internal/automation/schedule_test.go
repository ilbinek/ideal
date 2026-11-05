package automation

import (
	"errors"
	"testing"
	"time"

	"arma3modmanager/internal/database"
)

const allDays = 127

// 2026-11-05 is a Thursday. The slot is 06:00 UTC, every day.
var (
	slotTime = time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC)
	daily    = []database.RestartSlot{{Minute: 360, Days: allDays}}
)

// evalInput is a server that's been up for hours, with the slot not yet handled.
func evalInput(now time.Time) ScheduleInput {
	return ScheduleInput{
		Now:              now,
		CoordinatorStart: slotTime.Add(-24 * time.Hour),
		Slots:            daily,
		WarnMinutes:      []int{15, 5, 1},
		LastSlotUnix:     slotTime.Add(-24 * time.Hour).Unix(),
		Running:          true,
		StartedAt:        slotTime.Add(-5 * time.Hour),
		Warned:           func(int64, int) bool { return false },
	}
}

func warnedSet(keys ...int) func(int64, int) bool {
	return func(_ int64, minutes int) bool {
		for _, k := range keys {
			if k == minutes {
				return true
			}
		}
		return false
	}
}

func TestLatestAndNextSlot(t *testing.T) {
	if got := latestSlot(daily, slotTime); !got.Equal(slotTime) {
		t.Fatalf("at the slot: %s", got)
	}
	if got := latestSlot(daily, slotTime.Add(-time.Second)); !got.Equal(slotTime.AddDate(0, 0, -1)) {
		t.Fatalf("just before: %s", got)
	}
	if got := nextSlot(daily, slotTime); !got.Equal(slotTime.AddDate(0, 0, 1)) {
		t.Fatalf("next from the slot itself: %s", got)
	}

	// Thursdays only, from the following Wednesday: last week's, and tomorrow's.
	thursday := []database.RestartSlot{{Minute: 360, Days: 1 << int(time.Thursday)}}
	wednesday := slotTime.AddDate(0, 0, 6)
	if got := latestSlot(thursday, wednesday); !got.Equal(slotTime) {
		t.Fatalf("weekly latest: %s", got)
	}
	if got := nextSlot(thursday, wednesday); !got.Equal(slotTime.AddDate(0, 0, 7)) {
		t.Fatalf("weekly next: %s", got)
	}

	if got := latestSlot(nil, slotTime); !got.IsZero() {
		t.Fatalf("no slots: %s", got)
	}
	if got := nextSlot([]database.RestartSlot{{Minute: 1, Days: 0}}, slotTime); !got.IsZero() {
		t.Fatalf("no days: %s", got)
	}

	// Two times on the same day pick the right one.
	two := []database.RestartSlot{{Minute: 360, Days: allDays}, {Minute: 1080, Days: allDays}}
	if got := nextSlot(two, slotTime.Add(time.Hour)); !got.Equal(slotTime.Add(12 * time.Hour)) {
		t.Fatalf("second time of day: %s", got)
	}
	if got := latestSlot(two, slotTime.Add(13*time.Hour)); !got.Equal(slotTime.Add(12 * time.Hour)) {
		t.Fatalf("latest of two: %s", got)
	}
}

func TestEval_FiresOnce(t *testing.T) {
	in := evalInput(slotTime.Add(time.Second))
	d := EvalSchedule(in)
	if !d.Fire || d.MarkSlot != slotTime.Unix() {
		t.Fatalf("%+v", d)
	}
	in.LastSlotUnix = d.MarkSlot
	in.Now = in.Now.Add(time.Second)
	if d := EvalSchedule(in); d.Fire || d.MarkSlot != 0 {
		t.Fatalf("fired twice: %+v", d)
	}
}

func TestEval_RestartSameMinute(t *testing.T) {
	// Ideal restarted right after firing: the persisted mark stops a second restart.
	in := evalInput(slotTime.Add(10 * time.Second))
	in.CoordinatorStart = slotTime.Add(5 * time.Second)
	in.LastSlotUnix = slotTime.Unix()
	if d := EvalSchedule(in); d.Fire || d.Missed || d.MarkSlot != 0 {
		t.Fatalf("%+v", d)
	}
	// Came up in the same minute, before the slot had been handled: too risky to
	// fire, so it counts as missed.
	in.LastSlotUnix = slotTime.Add(-24 * time.Hour).Unix()
	if d := EvalSchedule(in); d.Fire || !d.Missed {
		t.Fatalf("%+v", d)
	}
}

func TestEval_SleepMissed(t *testing.T) {
	d := EvalSchedule(evalInput(slotTime.Add(61 * time.Second)))
	if d.Fire || !d.Missed || d.MarkSlot != slotTime.Unix() {
		t.Fatalf("%+v", d)
	}
	// On the edge of the window it still fires.
	if d := EvalSchedule(evalInput(slotTime.Add(slotFireWindow))); !d.Fire {
		t.Fatalf("%+v", d)
	}
}

func TestEval_SavedAfterSlot(t *testing.T) {
	// Saving sets the mark to the save time, so a slot just before it doesn't fire.
	in := evalInput(slotTime.Add(30 * time.Second))
	in.LastSlotUnix = slotTime.Add(20 * time.Second).Unix()
	if d := EvalSchedule(in); d.Fire || d.Missed || d.MarkSlot != 0 {
		t.Fatalf("%+v", d)
	}
}

func TestEval_SkipReasons(t *testing.T) {
	cases := map[string]func(*ScheduleInput){
		"the server was not running":        func(in *ScheduleInput) { in.Running = false },
		"automation is paused":              func(in *ScheduleInput) { in.Paused = true },
		"another operation was in progress": func(in *ScheduleInput) { in.Busy = true },
	}
	for want, change := range cases {
		in := evalInput(slotTime.Add(time.Second))
		change(&in)
		d := EvalSchedule(in)
		if d.Fire || d.SkipReason != want || d.MarkSlot == 0 {
			t.Errorf("%s: %+v", want, d)
		}
	}
}

func TestEval_MinUptimeSkip(t *testing.T) {
	in := evalInput(slotTime.Add(time.Second))
	in.StartedAt = slotTime.Add(-9 * time.Minute)
	if d := EvalSchedule(in); d.Fire || d.SkipReason == "" {
		t.Fatalf("%+v", d)
	}
	in.StartedAt = in.Now.Add(-minUptimeForRestart)
	if d := EvalSchedule(in); !d.Fire {
		t.Fatalf("exactly at the minimum uptime: %+v", d)
	}
}

func TestEval_PausedNoWarn(t *testing.T) {
	in := evalInput(slotTime.Add(-15 * time.Minute))
	in.Paused = true
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("%+v", d.Warnings)
	}
}

func TestEval_WarningsAtTheirTimes(t *testing.T) {
	in := evalInput(slotTime.Add(-16 * time.Minute))
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("too early: %+v", d.Warnings)
	}
	in.Now = slotTime.Add(-15 * time.Minute)
	d := EvalSchedule(in)
	if len(d.Warnings) != 1 || d.Warnings[0].Minutes != 15 || d.Warnings[0].Silent || d.Warnings[0].SlotUnix != slotTime.Unix() {
		t.Fatalf("%+v", d.Warnings)
	}
	in.Warned = warnedSet(15)
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("sent twice: %+v", d.Warnings)
	}
	in.Now = slotTime.Add(-5 * time.Minute)
	d = EvalSchedule(in)
	if len(d.Warnings) != 1 || d.Warnings[0].Minutes != 5 {
		t.Fatalf("%+v", d.Warnings)
	}
}

func TestEval_NoWarningsWhenStopped(t *testing.T) {
	in := evalInput(slotTime.Add(-5 * time.Minute))
	in.Running = false
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("%+v", d.Warnings)
	}
}

func TestEval_SavedSevenMinutesBefore(t *testing.T) {
	// The 15 is long gone and is dropped; 5 and 1 still fire at their own times.
	in := evalInput(slotTime.Add(-7 * time.Minute))
	d := EvalSchedule(in)
	if len(d.Warnings) != 1 || d.Warnings[0].Key != 15 || !d.Warnings[0].Silent {
		t.Fatalf("%+v", d.Warnings)
	}
	in.Warned = warnedSet(15)
	in.Now = slotTime.Add(-5 * time.Minute)
	d = EvalSchedule(in)
	if len(d.Warnings) != 1 || d.Warnings[0].Minutes != 5 || d.Warnings[0].Silent {
		t.Fatalf("%+v", d.Warnings)
	}
}

func TestEval_LateSaveCatchUpWarning(t *testing.T) {
	// 30 seconds left: the 1 minute warning is only a little late, so it still goes
	// out as itself, and no catch-up is needed on top of it.
	in := evalInput(slotTime.Add(-30 * time.Second))
	var sent []WarningDue
	for _, w := range EvalSchedule(in).Warnings {
		if !w.Silent {
			sent = append(sent, w)
		}
	}
	if len(sent) != 1 || sent[0].Key != 1 || sent[0].Minutes != 1 {
		t.Fatalf("%+v", sent)
	}

	// 90 seconds left and the smallest warning is 5 minutes, so it's far too late to
	// say "5 minutes". Nothing has gone out, so warn now with the time really left.
	in = evalInput(slotTime.Add(-90 * time.Second))
	in.WarnMinutes = []int{5}
	d := EvalSchedule(in)
	if len(d.Warnings) != 2 || !d.Warnings[0].Silent || d.Warnings[1].Key != catchUpMinutes || d.Warnings[1].Minutes != 2 {
		t.Fatalf("%+v", d.Warnings)
	}

	// Only once.
	in.Warned = warnedSet(5, catchUpMinutes)
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("sent again: %+v", d.Warnings)
	}

	// If the 5 minute warning did go out on time, players already know.
	in.Warned = warnedSet(5)
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("no catch-up after a real warning: %+v", d.Warnings)
	}
}

func TestEval_StartedAfterWarning(t *testing.T) {
	// Someone restarted it by hand after the 15 minute warning. The next slot will be
	// skipped for short uptime, so further warnings would be false.
	in := evalInput(slotTime.Add(-5 * time.Minute))
	in.StartedAt = slotTime.Add(-7 * time.Minute)
	in.Warned = warnedSet(15)
	if d := EvalSchedule(in); len(d.Warnings) != 0 {
		t.Fatalf("%+v", d.Warnings)
	}
	in.Now = slotTime.Add(time.Second)
	if d := EvalSchedule(in); d.Fire || d.SkipReason == "" {
		t.Fatalf("%+v", d)
	}
}

func TestFormatWarning(t *testing.T) {
	if got := FormatWarning("Restart in {minutes} min, {minutes}!", 5); got != "Restart in 5 min, 5!" {
		t.Fatal(got)
	}
	if got := FormatWarning("No placeholder", 5); got != "No placeholder" {
		t.Fatal(got)
	}
}

// --- coordinator behaviour

func (h *harness) schedule(restartOnCrash bool) {
	h.t.Helper()
	if err := database.SaveServerAutomation(h.id, false, restartOnCrash, true, []int{15, 5, 1}, daily, slotTime.Add(-time.Hour).Unix()); err != nil {
		h.t.Fatal(err)
	}
	h.c.startedAt = slotTime.Add(-time.Hour)
	h.ops.running[h.id] = true
	h.ops.startedAt[h.id] = slotTime.Add(-5 * time.Hour)
}

func TestPlannedRestartFiresOnce(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)
	if h.ops.restarts != 1 {
		t.Fatalf("restarts %d", h.ops.restarts)
	}
	h.advance(time.Second)
	h.advance(time.Minute)
	if h.ops.restarts != 1 {
		t.Fatalf("fired again: %d", h.ops.restarts)
	}
	if n := h.eventCount(database.EventPlannedRestart); n != 1 {
		t.Fatalf("events %d", n)
	}
	if a := h.automation(); a.OwedStart != database.OwedNone {
		t.Fatalf("owed %q", a.OwedStart)
	}
	if h.notifiedCount() != 0 {
		t.Fatalf("planned restarts don't notify: %v", h.notified)
	}
}

func TestPlannedRestartMarksOwedBeforeStopping(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	var owedDuringRestart string
	h.c.ops = &observingOps{fakeOps: h.ops, onRestart: func() {
		a, _ := database.GetServerAutomation(h.id)
		owedDuringRestart = a.OwedStart
	}}
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)
	if owedDuringRestart != database.OwedPlanned {
		t.Fatalf("owed during the restart: %q", owedDuringRestart)
	}
}

// observingOps runs a callback at the moment the stop would begin, after the guard.
type observingOps struct {
	*fakeOps
	onRestart func()
}

func (o *observingOps) RestartPlanned(id int, guard func() error) (bool, error) {
	if err := guard(); err != nil {
		return false, err
	}
	o.onRestart()
	return o.fakeOps.RestartPlanned(id, nil)
}

func TestPlannedRestartSkipsStoppedServer(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	h.ops.running[h.id] = false
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)
	if h.ops.restarts != 0 || h.ops.starts != 0 {
		t.Fatalf("a schedule must never start a stopped server: restarts %d, starts %d", h.ops.restarts, h.ops.starts)
	}
	if n := h.eventCount(database.EventPlannedSkipped); n != 1 {
		t.Fatalf("skip events %d", n)
	}
}

func TestPlannedStartFailCrashOff(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	h.ops.startErr = errors.New("port busy")
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)

	a := h.automation()
	if a.OwedStart != database.OwedNone || len(a.CrashHistory) != 0 {
		t.Fatalf("no retries with crash restart off: %+v", a)
	}
	if h.eventCount(database.EventStartFailed) != 1 || h.notifiedCount() != 1 {
		t.Fatalf("events/notifications: %d %v", h.eventCount(database.EventStartFailed), h.notified)
	}
	h.ops.running[h.id] = false
	h.advance(time.Hour)
	if h.ops.starts != 0 || h.ops.restarts != 1 {
		t.Fatalf("retried: starts %d restarts %d", h.ops.starts, h.ops.restarts)
	}
}

func TestPlannedStartFailCrashOn(t *testing.T) {
	h := newHarness(t)
	h.schedule(true)
	h.ops.startErr = errors.New("port busy")
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)

	a := h.automation()
	if a.OwedStart != database.OwedCrash || len(a.CrashHistory) != 1 {
		t.Fatalf("should enter backoff: %+v", a)
	}
	h.ops.startErr = nil
	h.ops.running[h.id] = false
	h.advance(10 * time.Second)
	if h.ops.starts != 1 {
		t.Fatalf("starts %d", h.ops.starts)
	}
}

func TestPlannedStopFailureLeavesServerUp(t *testing.T) {
	h := newHarness(t)
	h.schedule(true)
	h.c.ops = &stopFailOps{h.ops}
	h.clock = slotTime.Add(-time.Second)
	h.advance(2 * time.Second)
	a := h.automation()
	if a.OwedStart != database.OwedNone || len(a.CrashHistory) != 0 {
		t.Fatalf("a failed stop isn't a crash, and nothing is owed: %+v", a)
	}
	if h.eventCount(database.EventPlannedSkipped) != 1 {
		t.Fatal("expected a recorded skip")
	}
}

type stopFailOps struct{ *fakeOps }

func (o *stopFailOps) RestartPlanned(int, func() error) (bool, error) {
	return false, errors.New("process refused to die")
}

func TestWarningsSentOnceAndRecorded(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	h.clock = slotTime.Add(-15*time.Minute - time.Second)
	h.advance(time.Second)
	h.advance(time.Second)
	h.advance(time.Second)
	if len(h.ops.messages) != 1 {
		t.Fatalf("messages %v", h.ops.messages)
	}
	if want := FormatWarning(database.DefaultWarningTemplate, 15); h.ops.messages[0] != want {
		t.Fatalf("message %q, want %q", h.ops.messages[0], want)
	}
	if n := h.eventCount(database.EventWarningSent); n != 1 {
		t.Fatalf("events %d", n)
	}
}

func TestStatusShowsNextRestart(t *testing.T) {
	h := newHarness(t)
	h.schedule(false)
	h.clock = slotTime.Add(-time.Hour)
	h.c.step()
	if got := h.c.view.Load().servers[h.id].nextRestartAt; got != slotTime.Unix() {
		t.Fatalf("next restart %d, want %d", got, slotTime.Unix())
	}
}
