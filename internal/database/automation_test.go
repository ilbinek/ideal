package database

import (
	"fmt"
	"testing"
)

func newAutomationDB(t *testing.T) int {
	t.Helper()
	t.Setenv("IDEAL_DATA_DIR", t.TempDir())
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	branches, err := GetBranches()
	if err != nil || len(branches) == 0 {
		t.Fatalf("branches: %v", err)
	}
	id, err := CreateServer("Main", "main", branches[0].ID, nil, 42302, 42303, "Server", "")
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func TestAutomationDefaults(t *testing.T) {
	id := newAutomationDB(t)
	a, err := GetServerAutomation(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Autostart || a.RestartOnCrash || a.ScheduleEnabled || a.UserStopped {
		t.Fatalf("everything must default to off: %+v", a)
	}
	if len(a.WarnMinutes) != 3 || a.WarnMinutes[0] != 15 {
		t.Fatalf("warnings %v", a.WarnMinutes)
	}
	s, err := GetAutomationSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Paused || s.BootDelaySec != 30 || s.BootGapSec != 30 || s.WarningTemplate != DefaultWarningTemplate {
		t.Fatalf("%+v", s)
	}
}

func TestSavingSettingsKeepsIntent(t *testing.T) {
	id := newAutomationDB(t)
	if err := RecordManualStop(id); err != nil {
		t.Fatal(err)
	}
	if err := SetCrashState(id, []int64{1, 2}, OwedCrash, 99); err != nil {
		t.Fatal(err)
	}
	if err := SaveServerAutomation(id, true, true, false, []int{5}, []RestartSlot{{Minute: 60, Days: 1}}, 1234); err != nil {
		t.Fatal(err)
	}
	a, err := GetServerAutomation(id)
	if err != nil {
		t.Fatal(err)
	}
	if !a.UserStopped || a.OwedStart != OwedCrash || a.GaveUpAt != 99 || len(a.CrashHistory) != 2 {
		t.Fatalf("saving settings changed intent: %+v", a)
	}
	if !a.Autostart || a.RestartSlots[0].Minute != 60 || a.LastSlotUnix != 1234 {
		t.Fatalf("%+v", a)
	}
}

func TestIntentTransitions(t *testing.T) {
	id := newAutomationDB(t)
	get := func() *ServerAutomation {
		t.Helper()
		a, err := GetServerAutomation(id)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	if err := SetCrashState(id, []int64{1}, OwedCrash, 50); err != nil {
		t.Fatal(err)
	}
	if err := RecordManualStop(id); err != nil {
		t.Fatal(err)
	}
	if a := get(); !a.UserStopped || a.OwedStart != OwedNone {
		t.Fatalf("stop: %+v", a)
	}

	if err := RecordManualRestart(id); err != nil {
		t.Fatal(err)
	}
	if a := get(); a.UserStopped || a.OwedStart != OwedManual || a.GaveUpAt != 0 || len(a.CrashHistory) != 0 {
		t.Fatalf("restart: %+v", a)
	}

	if err := SetCrashState(id, []int64{1}, OwedCrash, 50); err != nil {
		t.Fatal(err)
	}
	if err := RecordManualStop(id); err != nil {
		t.Fatal(err)
	}
	if err := RecordManualStart(id); err != nil {
		t.Fatal(err)
	}
	if a := get(); a.UserStopped || a.OwedStart != OwedNone || a.GaveUpAt != 0 || len(a.CrashHistory) != 0 {
		t.Fatalf("start: %+v", a)
	}
}

func TestEventsPrune(t *testing.T) {
	id := newAutomationDB(t)
	const extra = 7
	for i := 0; i < maxEventsPerServer+extra; i++ {
		if err := AddServerEvent(id, EventStarted, "system", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ListServerEvents(id, maxEventsPerServer, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != maxEventsPerServer {
		t.Fatalf("kept %d events", len(first))
	}
	if first[0].Detail != fmt.Sprint(maxEventsPerServer+extra-1) {
		t.Fatalf("newest first expected, got %q", first[0].Detail)
	}
	if last := first[len(first)-1].Detail; last != fmt.Sprint(extra) {
		t.Fatalf("oldest kept is %q", last)
	}

	page, err := ListServerEvents(id, 2, first[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != first[2].ID {
		t.Fatalf("paging: %+v", page)
	}
}

func TestDeleteServerCascadesAutomation(t *testing.T) {
	id := newAutomationDB(t)
	if err := SaveServerAutomation(id, true, true, false, []int{5}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := AddServerEvent(id, EventStarted, "system", ""); err != nil {
		t.Fatal(err)
	}
	if err := DeleteServer(id); err != nil {
		t.Fatal(err)
	}
	all, err := GetAllServerAutomation()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("rows left: %v", all)
	}
	events, err := ListServerEvents(id, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events left: %v", events)
	}
}
