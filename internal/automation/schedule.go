package automation

import (
	"time"

	"arma3modmanager/internal/database"
)

// warnKey identifies one warning for one restart, so it's only sent once.
type warnKey struct {
	serverID int
	slotUnix int64
	minutes  int
}

func (c *Coordinator) stepSchedules(time.Time, *database.AutomationSettings, map[int]*database.ServerAutomation) {
}

func (c *Coordinator) onWarningResult(warningResult) {}

func nextRestartFor(time.Time, *database.ServerAutomation) int64 { return 0 }
