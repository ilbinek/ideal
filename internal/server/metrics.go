package server

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

// ProcessMetrics is a point-in-time sample of a managed Arma process.
type ProcessMetrics struct {
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryBytes uint64  `json:"memoryBytes"`
	Threads     int32   `json:"threads"`
}

type cpuSample struct {
	times cpu.TimesStat
	at    time.Time
}

var (
	metricsMu  sync.Mutex
	cpuCache   = map[int]cpuSample{}
	activePIDs = map[int]struct{}{} // PIDs seen in the last Snapshot pass
)

// sampleProcess returns live CPU/memory/thread stats for pid.
// CPU% is the interval usage since the previous sample for this PID
// (0 on the first sample). Returns nil if the process cannot be queried.
func sampleProcess(pid int) *ProcessMetrics {
	if pid <= 0 {
		return nil
	}

	metricsMu.Lock()
	activePIDs[pid] = struct{}{}
	metricsMu.Unlock()

	p, err := process.NewProcess(int32(pid))
	if err != nil {
		dropCPUCache(pid)
		return nil
	}

	mem, err := p.MemoryInfo()
	if err != nil || mem == nil {
		dropCPUCache(pid)
		return nil
	}

	threads, _ := p.NumThreads()

	now := time.Now()
	times, err := p.Times()
	cpuPct := 0.0
	if err == nil && times != nil {
		metricsMu.Lock()
		prev, ok := cpuCache[pid]
		if ok {
			wall := now.Sub(prev.at).Seconds()
			if wall > 0 {
				delta := times.Total() - prev.times.Total()
				if delta < 0 {
					delta = 0
				}
				cpuPct = (delta / wall) * 100
			}
		}
		cpuCache[pid] = cpuSample{times: *times, at: now}
		metricsMu.Unlock()
	}

	return &ProcessMetrics{
		CPUPercent:  cpuPct,
		MemoryBytes: mem.RSS,
		Threads:     threads,
	}
}

func dropCPUCache(pid int) {
	metricsMu.Lock()
	delete(cpuCache, pid)
	metricsMu.Unlock()
}

// pruneMetricsCache removes CPU samples for PIDs not seen in the last Snapshot.
func pruneMetricsCache() {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	for pid := range cpuCache {
		if _, ok := activePIDs[pid]; !ok {
			delete(cpuCache, pid)
		}
	}
	activePIDs = map[int]struct{}{}
}
