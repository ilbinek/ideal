// Package automation starts and restarts servers without anyone asking: when Ideal
// launches, after unexpected exits, and on a schedule. The decisions are pure
// functions in this file and schedule.go, and the coordinator carries them out.
package automation

import (
	"time"

	"arma3modmanager/internal/database"
)

const (
	// failureWindow is how far back failures count towards giving up. A server that
	// stays up this long has shown it's healthy, which also clears its history.
	failureWindow = 15 * time.Minute
	// maxFailures within the window ends the retries.
	maxFailures = 5
)

// backoffDelays is the wait before each retry, by failure count.
var backoffDelays = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
}

// OnFailure records a failed run and decides what happens next. instStart is when
// the failed run began, or zero if the server never got as far as running.
func OnFailure(history []time.Time, now, instStart time.Time) (updated []time.Time, delay time.Duration, giveUp bool) {
	if !instStart.IsZero() && now.Sub(instStart) >= failureWindow {
		history = nil
	}
	updated = make([]time.Time, 0, len(history)+1)
	for _, at := range history {
		if now.Sub(at) < failureWindow {
			updated = append(updated, at)
		}
	}
	updated = append(updated, now)
	if len(updated) >= maxFailures {
		return updated, 0, true
	}
	return updated, backoffDelays[min(len(updated)-1, len(backoffDelays)-1)], false
}

// ServerView is what the boot decision needs to know about a server.
type ServerView struct {
	ID         int
	Up         bool // already running, so there's nothing to start
	Automation *database.ServerAutomation
}

// BootItem is a server to start after launch.
type BootItem struct {
	ServerID int
	Owed     string // why it's owed a start, or "" if only its toggle says so
}

// BootDecision is what to do with every server when Ideal launches.
type BootDecision struct {
	Start     []BootItem
	ClearOwed []int // servers whose pending start no longer applies
}

// BootPlan decides which servers to start when Ideal launches, keeping the servers'
// order. A person's deliberate stop and a give-up always win; after that an
// interrupted restart is finished, whatever the launch toggle says, because the
// schedule or the person who asked for it can't bring the server back themselves.
func BootPlan(servers []ServerView, paused bool) BootDecision {
	var d BootDecision
	for _, s := range servers {
		a := s.Automation
		owed := a.OwedStart

		if s.Up {
			// The stop half never happened, so the server is simply still running.
			if owed != database.OwedNone {
				d.ClearOwed = append(d.ClearOwed, s.ID)
			}
			continue
		}
		if a.UserStopped || a.GaveUpAt > 0 {
			if owed != database.OwedNone {
				d.ClearOwed = append(d.ClearOwed, s.ID)
			}
			continue
		}
		if paused {
			if owed != database.OwedNone {
				d.ClearOwed = append(d.ClearOwed, s.ID)
			}
			continue
		}
		switch owed {
		case database.OwedManual, database.OwedPlanned:
			d.Start = append(d.Start, BootItem{ServerID: s.ID, Owed: owed})
		default:
			// A crash retry doesn't carry over a relaunch: only the toggle decides.
			if owed != database.OwedNone {
				d.ClearOwed = append(d.ClearOwed, s.ID)
			}
			if a.Autostart {
				d.Start = append(d.Start, BootItem{ServerID: s.ID})
			}
		}
	}
	return d
}

func unixTimes(history []int64) []time.Time {
	out := make([]time.Time, len(history))
	for i, at := range history {
		out[i] = time.Unix(at, 0)
	}
	return out
}

func toUnix(history []time.Time) []int64 {
	out := make([]int64, len(history))
	for i, at := range history {
		out[i] = at.Unix()
	}
	return out
}
