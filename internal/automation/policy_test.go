package automation

import (
	"testing"
	"time"

	"arma3modmanager/internal/database"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestOnFailure_Delays(t *testing.T) {
	want := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	var hist []time.Time
	now := t0
	for i, w := range want {
		var delay time.Duration
		var giveUp bool
		hist, delay, giveUp = OnFailure(hist, now, now.Add(-time.Second))
		if giveUp || delay != w {
			t.Fatalf("failure %d: delay %s giveUp %v", i+1, delay, giveUp)
		}
		now = now.Add(delay)
	}
}

func TestOnFailure_FifthGivesUp(t *testing.T) {
	var hist []time.Time
	for i := 0; i < maxFailures-1; i++ {
		var giveUp bool
		hist, _, giveUp = OnFailure(hist, t0.Add(time.Duration(i)*time.Second), time.Time{})
		if giveUp {
			t.Fatalf("gave up after %d failures", i+1)
		}
	}
	if _, _, giveUp := OnFailure(hist, t0.Add(time.Minute), time.Time{}); !giveUp {
		t.Fatal("fifth failure within the window should give up")
	}
}

func TestOnFailure_WindowSlides(t *testing.T) {
	// Four old failures that have all aged out don't count towards the next one.
	hist := []time.Time{t0, t0.Add(time.Minute), t0.Add(2 * time.Minute), t0.Add(3 * time.Minute)}
	now := t0.Add(3*time.Minute + failureWindow)
	updated, delay, giveUp := OnFailure(hist, now, time.Time{})
	if giveUp || len(updated) != 1 || delay != 10*time.Second {
		t.Fatalf("updated %v delay %s giveUp %v", updated, delay, giveUp)
	}
	// Exactly at the edge an entry has aged out; just inside it still counts.
	updated, _, _ = OnFailure([]time.Time{t0.Add(time.Second)}, t0.Add(failureWindow), time.Time{})
	if len(updated) != 2 {
		t.Fatalf("entry inside the window was dropped: %v", updated)
	}
}

func TestOnFailure_StableResets(t *testing.T) {
	hist := []time.Time{t0, t0.Add(time.Second), t0.Add(2 * time.Second), t0.Add(3 * time.Second)}
	now := t0.Add(4 * time.Second)
	// Would be the fifth, but this run stayed up long enough to prove itself. The
	// failures are kept recent on purpose, so only the stable run can clear them.
	updated, delay, giveUp := OnFailure(hist, now, now.Add(-failureWindow))
	if giveUp || len(updated) != 1 || delay != 10*time.Second {
		t.Fatalf("updated %v delay %s giveUp %v", updated, delay, giveUp)
	}
	// A run that never started isn't stable, however old anything else is.
	if _, _, giveUp := OnFailure(hist, now, time.Time{}); !giveUp {
		t.Fatal("a failed start must not count as a stable run")
	}
}

func view(id int, up bool, a database.ServerAutomation) ServerView {
	return ServerView{ID: id, Up: up, Automation: &a}
}

func starts(d BootDecision) []int {
	ids := make([]int, 0, len(d.Start))
	for _, s := range d.Start {
		ids = append(ids, s.ServerID)
	}
	return ids
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBootPlan_Autostart(t *testing.T) {
	d := BootPlan([]ServerView{
		view(1, false, database.ServerAutomation{Autostart: true}),
		view(2, false, database.ServerAutomation{}),
		view(3, false, database.ServerAutomation{Autostart: true}),
		view(4, true, database.ServerAutomation{Autostart: true}), // adopted, still running
	}, false)
	if !sameInts(starts(d), []int{1, 3}) {
		t.Fatalf("start %v", starts(d))
	}
}

func TestBootPlan_UserStopped(t *testing.T) {
	d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{Autostart: true, UserStopped: true})}, false)
	if len(d.Start) != 0 {
		t.Fatalf("a deliberately stopped server must stay off: %v", d.Start)
	}
}

func TestBootPlan_CrashToggleDoesNotStart(t *testing.T) {
	// Toggles are independent: crash restart alone doesn't survive a power loss.
	d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{RestartOnCrash: true})}, false)
	if len(d.Start) != 0 {
		t.Fatalf("start %v", d.Start)
	}
}

func TestBootPlan_OwedPlannedNoAutostart(t *testing.T) {
	// Ideal died between a planned restart's stop and start: finish it anyway.
	for _, owed := range []string{database.OwedPlanned, database.OwedManual} {
		d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{OwedStart: owed})}, false)
		if !sameInts(starts(d), []int{1}) || d.Start[0].Owed != owed {
			t.Fatalf("owed %q: %+v", owed, d)
		}
	}
}

func TestBootPlan_OwedCrashFollowsLaunchToggle(t *testing.T) {
	d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{OwedStart: database.OwedCrash, RestartOnCrash: true})}, false)
	if len(d.Start) != 0 || !sameInts(d.ClearOwed, []int{1}) {
		t.Fatalf("launch off: %+v", d)
	}
	d = BootPlan([]ServerView{view(1, false, database.ServerAutomation{OwedStart: database.OwedCrash, Autostart: true})}, false)
	if !sameInts(starts(d), []int{1}) || !sameInts(d.ClearOwed, []int{1}) {
		t.Fatalf("launch on: %+v", d)
	}
}

func TestBootPlan_GaveUp(t *testing.T) {
	d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{Autostart: true, GaveUpAt: 123})}, false)
	if len(d.Start) != 0 {
		t.Fatalf("start %v", d.Start)
	}
}

func TestBootPlan_Paused(t *testing.T) {
	d := BootPlan([]ServerView{
		view(1, false, database.ServerAutomation{Autostart: true}),
		view(2, false, database.ServerAutomation{OwedStart: database.OwedPlanned}),
	}, true)
	if len(d.Start) != 0 {
		t.Fatalf("start %v", d.Start)
	}
	if !sameInts(d.ClearOwed, []int{2}) {
		t.Fatalf("no catch-up after unpausing, so owed should clear: %v", d.ClearOwed)
	}
}

func TestBootPlan_RunningWithOwedClears(t *testing.T) {
	// Marked for a restart but still running: the stop never began.
	d := BootPlan([]ServerView{view(1, true, database.ServerAutomation{OwedStart: database.OwedPlanned})}, false)
	if len(d.Start) != 0 || !sameInts(d.ClearOwed, []int{1}) {
		t.Fatalf("%+v", d)
	}
}

func TestBootPlan_UserStopBeatsOwed(t *testing.T) {
	d := BootPlan([]ServerView{view(1, false, database.ServerAutomation{UserStopped: true, OwedStart: database.OwedPlanned})}, false)
	if len(d.Start) != 0 || !sameInts(d.ClearOwed, []int{1}) {
		t.Fatalf("%+v", d)
	}
}
