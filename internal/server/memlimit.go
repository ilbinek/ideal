package server

import (
	"fmt"
	"sync"
)

const (
	// MinMemoryLimitMB is the smallest enforceable cap (below this is treated as invalid).
	MinMemoryLimitMB = 256
	// MaxMemoryLimitMB guards against absurd values.
	MaxMemoryLimitMB = 1024 * 1024 // 1 TiB
)

var (
	memLimitMu sync.Mutex
	memLimits  = map[int]*memLimitHandle{}
)

// ValidateMemoryLimitMB returns a normalized limit or an error.
// 0 means unlimited.
func ValidateMemoryLimitMB(mb int) (int, error) {
	if mb < 0 {
		return 0, fmt.Errorf("memoryLimitMB cannot be negative")
	}
	if mb == 0 {
		return 0, nil
	}
	if mb < MinMemoryLimitMB {
		return 0, fmt.Errorf("memoryLimitMB must be 0 (unlimited) or at least %d", MinMemoryLimitMB)
	}
	if mb > MaxMemoryLimitMB {
		return 0, fmt.Errorf("memoryLimitMB cannot exceed %d", MaxMemoryLimitMB)
	}
	return mb, nil
}

// ApplyServerMemoryLimit attaches pid to this server's shared memory budget.
// Dedicated and headless clients for the same serverID share one limit.
// limitMB 0 clears any previous limit tracking for this server (no OS cap applied).
func ApplyServerMemoryLimit(serverID, pid, limitMB int) error {
	if pid <= 0 {
		return nil
	}
	limitMB, err := ValidateMemoryLimitMB(limitMB)
	if err != nil {
		return err
	}

	memLimitMu.Lock()
	defer memLimitMu.Unlock()

	if limitMB == 0 {
		releaseMemoryLimitLocked(serverID)
		return nil
	}

	h := memLimits[serverID]
	if h != nil && h.limitMB != limitMB {
		releaseMemoryLimitLocked(serverID)
		h = nil
	}
	if h == nil {
		h, err = createMemoryLimit(serverID, limitMB)
		if err != nil {
			return err
		}
		memLimits[serverID] = h
	}
	return h.attach(pid)
}

// ReleaseServerMemoryLimit drops Ideal's handle for the server's memory limit.
// Does not kill processes (Windows job is created without KILL_ON_JOB_CLOSE).
func ReleaseServerMemoryLimit(serverID int) {
	memLimitMu.Lock()
	defer memLimitMu.Unlock()
	releaseMemoryLimitLocked(serverID)
}

func releaseMemoryLimitLocked(serverID int) {
	if h, ok := memLimits[serverID]; ok {
		h.close()
		delete(memLimits, serverID)
	}
}
