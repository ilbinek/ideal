package automation

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/server"
)

const (
	// slotFireWindow is how late a slot can still fire. Later than this, Ideal was
	// down or the host was asleep, and restarting out of the blue would be worse than
	// waiting for the next one.
	slotFireWindow = 60 * time.Second
	// minUptimeForRestart protects a server started just before a slot from being
	// restarted again straight away.
	minUptimeForRestart = 10 * time.Minute
	// warnGrace is how late a warning can still go out and be accurate.
	warnGrace = 60 * time.Second
	// scheduleHorizonDays is far enough to find any weekday's next or last slot.
	scheduleHorizonDays = 8
)

// warnKey identifies one warning for one restart, so it's only sent once.
type warnKey struct {
	serverID int
	slotUnix int64
	minutes  int
}

// catchUpMinutes marks the catch-up warning in warnKey, which has no offset of its own.
const catchUpMinutes = 0

func slotInstance(day time.Time, slot database.RestartSlot) (time.Time, bool) {
	if slot.Days&(1<<int(day.Weekday())) == 0 {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), 0, slot.Minute, 0, 0, time.UTC), true
}

// latestSlot returns the newest slot time at or before now, or zero if there's none.
func latestSlot(slots []database.RestartSlot, now time.Time) time.Time {
	now = now.UTC()
	var latest time.Time
	for offset := 0; offset < scheduleHorizonDays; offset++ {
		day := now.AddDate(0, 0, -offset)
		for _, slot := range slots {
			if at, ok := slotInstance(day, slot); ok && !at.After(now) && at.After(latest) {
				latest = at
			}
		}
		if !latest.IsZero() {
			return latest
		}
	}
	return latest
}

// nextSlot returns the earliest slot time after now, or zero if there's none.
func nextSlot(slots []database.RestartSlot, now time.Time) time.Time {
	now = now.UTC()
	var next time.Time
	for offset := 0; offset < scheduleHorizonDays; offset++ {
		day := now.AddDate(0, 0, offset)
		for _, slot := range slots {
			if at, ok := slotInstance(day, slot); ok && at.After(now) && (next.IsZero() || at.Before(next)) {
				next = at
			}
		}
		if !next.IsZero() {
			return next
		}
	}
	return next
}

// ScheduleInput is everything the schedule decision depends on.
type ScheduleInput struct {
	Now              time.Time
	CoordinatorStart time.Time
	Slots            []database.RestartSlot
	WarnMinutes      []int // descending
	LastSlotUnix     int64
	Running          bool
	StartedAt        time.Time
	Paused           bool
	Busy             bool
	// Warned reports whether a warning for this restart has already been handled.
	Warned func(slotUnix int64, minutes int) bool
}

// WarningDue is a warning to send: Minutes is what to tell players, and Key is what
// to remember it by. Silent warnings are too late to send but still get remembered.
type WarningDue struct {
	SlotUnix int64
	Key      int
	Minutes  int
	Silent   bool
}

// ScheduleDecision is what to do about a server's schedule right now.
type ScheduleDecision struct {
	MarkSlot   int64 // persist this as handled before acting, so a slot only fires once
	Fire       bool
	SkipReason string
	Missed     bool
	Warnings   []WarningDue
}

// EvalSchedule decides what a server's schedule calls for at this moment.
func EvalSchedule(in ScheduleInput) ScheduleDecision {
	var d ScheduleDecision

	if latest := latestSlot(in.Slots, in.Now); !latest.IsZero() && latest.Unix() > in.LastSlotUnix {
		d.MarkSlot = latest.Unix()
		switch {
		case in.Now.Sub(latest) > slotFireWindow || !latest.After(in.CoordinatorStart):
			d.Missed = true
		case in.Paused:
			d.SkipReason = "automation is paused"
		case in.Busy:
			d.SkipReason = "another operation was in progress"
		case !in.Running:
			d.SkipReason = "the server was not running"
		case in.Now.Sub(in.StartedAt) < minUptimeForRestart:
			d.SkipReason = "the server had only just started"
		default:
			d.Fire = true
		}
	}

	if in.Running && !in.Paused {
		d.Warnings = dueWarnings(in)
	}
	return d
}

func dueWarnings(in ScheduleInput) []WarningDue {
	next := nextSlot(in.Slots, in.Now)
	if next.IsZero() || len(in.WarnMinutes) == 0 {
		return nil
	}
	// This server will skip that restart for having only just started, so warning
	// players about it would be telling them something untrue.
	if next.Sub(in.StartedAt) < minUptimeForRestart {
		return nil
	}

	slotUnix := next.Unix()
	var due []WarningDue
	smallest := in.WarnMinutes[0]
	anyHandled := in.Warned(slotUnix, catchUpMinutes)
	for _, minutes := range in.WarnMinutes {
		smallest = min(smallest, minutes)
		if in.Warned(slotUnix, minutes) {
			anyHandled = true
			continue
		}
		at := next.Add(-time.Duration(minutes) * time.Minute)
		if in.Now.Before(at) {
			continue
		}
		// Past its moment and too late to be accurate: remember it, don't send it.
		silent := in.Now.Sub(at) > warnGrace
		due = append(due, WarningDue{SlotUnix: slotUnix, Key: minutes, Minutes: minutes, Silent: silent})
		if !silent {
			anyHandled = true
		}
	}

	// Saved too close to the slot for any normal warning to go out, so send one now.
	remaining := next.Sub(in.Now)
	if !anyHandled && remaining < time.Duration(smallest)*time.Minute {
		minutes := int((remaining + time.Minute - time.Nanosecond) / time.Minute)
		due = append(due, WarningDue{SlotUnix: slotUnix, Key: catchUpMinutes, Minutes: minutes})
	}
	return due
}

// FormatWarning fills in a warning template.
func FormatWarning(template string, minutes int) string {
	return strings.ReplaceAll(template, "{minutes}", strconv.Itoa(minutes))
}

func (c *Coordinator) stepSchedules(now time.Time, settings *database.AutomationSettings, all map[int]*database.ServerAutomation) {
	for id, a := range all {
		if !a.ScheduleEnabled || len(a.RestartSlots) == 0 {
			continue
		}
		running, startedAt := c.ops.Running(id)
		decision := EvalSchedule(ScheduleInput{
			Now:              now,
			CoordinatorStart: c.startedAt,
			Slots:            a.RestartSlots,
			WarnMinutes:      a.WarnMinutes,
			LastSlotUnix:     a.LastSlotUnix,
			Running:          running,
			StartedAt:        startedAt,
			Paused:           settings.Paused,
			Busy:             c.inflight[id] || c.ops.CurrentOp(id) != "",
			Warned: func(slotUnix int64, minutes int) bool {
				return c.warned[warnKey{id, slotUnix, minutes}]
			},
		})
		c.applySchedule(id, settings, decision)
	}
	c.pruneWarned(now)
}

func (c *Coordinator) applySchedule(id int, settings *database.AutomationSettings, d ScheduleDecision) {
	if d.MarkSlot != 0 {
		if err := database.SetLastSlot(id, d.MarkSlot); err != nil {
			// Without this the slot could fire again on every tick.
			log.Printf("[AUTOMATION] saving schedule progress for server %d: %v", id, err)
			return
		}
	}
	for _, w := range d.Warnings {
		c.warned[warnKey{id, w.SlotUnix, w.Key}] = true
		if !w.Silent {
			c.sendWarning(id, FormatWarning(settings.WarningTemplate, w.Minutes), w.Minutes)
		}
	}
	switch {
	case d.Fire:
		c.runPlanned(id)
	case d.Missed:
		addEvent(id, database.EventPlannedMissed, "Ideal was not running at the scheduled time")
	case d.SkipReason != "":
		addEvent(id, database.EventPlannedSkipped, d.SkipReason)
	}
}

// pruneWarned forgets warnings for restarts that have passed.
func (c *Coordinator) pruneWarned(now time.Time) {
	for key := range c.warned {
		if key.slotUnix < now.Unix() {
			delete(c.warned, key)
		}
	}
}

func (c *Coordinator) sendWarning(serverID int, message string, minutes int) {
	c.spawn(func() {
		err := c.ops.SendRConMessage(serverID, message)
		c.post(event{warning: &warningResult{serverID: serverID, minutes: minutes, err: err}})
	})
}

func (c *Coordinator) onWarningResult(r warningResult) {
	switch {
	case r.err == nil:
		addEvent(r.serverID, database.EventWarningSent, fmt.Sprintf("%d minute(s) before the restart", r.minutes))
	case server.IsRConUnavailable(r.err):
		// Routine on servers without BattlEye: they restart without warning.
	default:
		log.Printf("[AUTOMATION] warning players on server %d: %v", r.serverID, r.err)
	}
}

// runPlanned restarts a server for its schedule. The guard re-checks everything with
// the server's lock held, then records that it's owed a start before the stop begins,
// so Ideal dying halfway can't leave it down.
func (c *Coordinator) runPlanned(serverID int) {
	c.inflight[serverID] = true
	c.spawn(func() {
		attempted, err := c.ops.RestartPlanned(serverID, func() error {
			a, paused, err := currentIntent(serverID)
			if err != nil {
				return err
			}
			if paused || a.UserStopped || !a.ScheduleEnabled {
				return errSkipped
			}
			if running, _ := c.ops.Running(serverID); !running {
				return errSkipped
			}
			return database.SetOwedStart(serverID, database.OwedPlanned)
		})
		c.post(event{result: &actionResult{
			serverID:  serverID,
			source:    sourcePlanned,
			owed:      database.OwedPlanned,
			attempted: attempted,
			err:       err,
		}})
	})
}

// onPlannedStopHalf handles a planned restart that ended before its start half. It
// reports whether it dealt with the result.
func (c *Coordinator) onPlannedStopHalf(r actionResult, a *database.ServerAutomation) bool {
	var busy *server.BusyError
	if r.source != sourcePlanned || r.attempted || errors.As(r.err, &busy) || errors.Is(r.err, errSkipped) {
		return false
	}
	if a.OwedStart == database.OwedPlanned {
		_ = database.SetOwedStart(r.serverID, database.OwedNone)
	}
	if r.err != nil {
		// The server is still up, so this needs noting rather than an alarm.
		addEvent(r.serverID, database.EventPlannedSkipped, "the server could not be stopped: "+r.err.Error())
	}
	// With no error, a person's Stop overrode the restart, and it's recorded their intent.
	return true
}

func nextRestartFor(now time.Time, a *database.ServerAutomation) int64 {
	if !a.ScheduleEnabled {
		return 0
	}
	next := nextSlot(a.RestartSlots, now)
	if next.IsZero() {
		return 0
	}
	return next.Unix()
}
