package server

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// ProcessMetrics is a point-in-time sample of a managed Arma process.
type ProcessMetrics struct {
	CPUPercent       float64 `json:"cpuPercent"`
	CPUCount         int     `json:"cpuCount"`
	MemoryBytes      uint64  `json:"memoryBytes"`
	MemoryTotalBytes uint64  `json:"memoryTotalBytes"`
	Threads          int32   `json:"threads"`
}

type cpuSample struct {
	times cpu.TimesStat
	at    time.Time
}

var (
	metricsMu  sync.Mutex
	cpuCache   = map[int]cpuSample{}
	activePIDs = map[int]struct{}{} // PIDs seen in the last Snapshot pass

	cachedCPUCount   atomic.Int32
	cachedMemTotal   atomic.Uint64
	hostInfoCachedAt atomic.Int64 // unix nano
)

const hostInfoTTL = 30 * time.Second

func hostInfo() (cpuCount int, memTotal uint64) {
	now := time.Now().UnixNano()
	prev := hostInfoCachedAt.Load()
	cpuCount = int(cachedCPUCount.Load())
	memTotal = cachedMemTotal.Load()
	if prev > 0 && now-prev < int64(hostInfoTTL) && cpuCount > 0 && memTotal > 0 {
		return cpuCount, memTotal
	}

	if n, err := cpu.Counts(true); err == nil && n > 0 {
		cpuCount = n
		cachedCPUCount.Store(int32(n))
	} else if cpuCount <= 0 {
		cpuCount = 1
	}

	if vm, err := mem.VirtualMemory(); err == nil && vm != nil && vm.Total > 0 {
		memTotal = vm.Total
		cachedMemTotal.Store(vm.Total)
	}

	hostInfoCachedAt.Store(now)
	return cpuCount, memTotal
}

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

	memInfo, err := p.MemoryInfo()
	if err != nil || memInfo == nil {
		dropCPUCache(pid)
		return nil
	}

	threads, _ := p.NumThreads()
	cpuCount, memTotal := hostInfo()

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
		CPUPercent:       cpuPct,
		CPUCount:         cpuCount,
		MemoryBytes:      memInfo.RSS,
		MemoryTotalBytes: memTotal,
		Threads:          threads,
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
