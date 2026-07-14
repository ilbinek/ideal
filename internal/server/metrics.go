package server

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	gpsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// ProcessMetrics is a point-in-time sample of a managed Arma process,
// plus host context used for gauges (RAM total, CPU count, NIC rates).
type ProcessMetrics struct {
	CPUPercent         float64 `json:"cpuPercent"`
	CPUCount           int     `json:"cpuCount"`
	HostCPUPercent     float64 `json:"hostCpuPercent"` // overall host 0–100
	MemoryBytes        uint64  `json:"memoryBytes"`
	MemoryUsedBytes    uint64  `json:"memoryUsedBytes"` // host used (all processes)
	MemoryTotalBytes   uint64  `json:"memoryTotalBytes"`
	Threads            int32   `json:"threads"`
	NetRecvBytesPerSec float64 `json:"netRecvBytesPerSec"`
	NetSentBytesPerSec float64 `json:"netSentBytesPerSec"`
}

type cpuSample struct {
	times cpu.TimesStat
	at    time.Time
}

type netSample struct {
	recv uint64
	sent uint64
	at   time.Time
}

type hostCPUSample struct {
	total float64
	idle  float64
	at    time.Time
}

// hostContext is shared host-level stats attached to every process sample.
type hostContext struct {
	CPUCount         int
	HostCPUPercent   float64
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	NetRecvBps       float64
	NetSentBps       float64
}

var (
	metricsMu  sync.Mutex
	cpuCache   = map[int]cpuSample{}
	activePIDs = map[int]struct{}{} // PIDs seen in the last Snapshot pass

	cachedCPUCount atomic.Int32

	netMu         sync.Mutex
	lastNetSample netSample

	hostCPUMu   sync.Mutex
	lastHostCPU hostCPUSample
)

func logicalCPUCount() int {
	if n := int(cachedCPUCount.Load()); n > 0 {
		return n
	}
	if n, err := cpu.Counts(true); err == nil && n > 0 {
		cachedCPUCount.Store(int32(n))
		return n
	}
	return 1
}

// sampleHostNet returns aggregate host NIC throughput (bytes/sec) since the
// previous call. First sample returns zeros.
func sampleHostNet() (recvBps, sentBps float64) {
	counters, err := gpsnet.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return 0, 0
	}
	cur := netSample{
		recv: counters[0].BytesRecv,
		sent: counters[0].BytesSent,
		at:   time.Now(),
	}

	netMu.Lock()
	defer netMu.Unlock()
	prev := lastNetSample
	lastNetSample = cur
	if prev.at.IsZero() {
		return 0, 0
	}
	wall := cur.at.Sub(prev.at).Seconds()
	if wall <= 0 {
		return 0, 0
	}
	if cur.recv >= prev.recv {
		recvBps = float64(cur.recv-prev.recv) / wall
	}
	if cur.sent >= prev.sent {
		sentBps = float64(cur.sent-prev.sent) / wall
	}
	return recvBps, sentBps
}

// sampleHostCPU returns overall host CPU utilization 0–100 since the previous call.
func sampleHostCPU() float64 {
	times, err := cpu.Times(false)
	if err != nil || len(times) == 0 {
		return 0
	}
	t := times[0]
	cur := hostCPUSample{
		total: t.Total(),
		idle:  t.Idle + t.Iowait,
		at:    time.Now(),
	}

	hostCPUMu.Lock()
	defer hostCPUMu.Unlock()
	prev := lastHostCPU
	lastHostCPU = cur
	if prev.at.IsZero() {
		return 0
	}
	dTotal := cur.total - prev.total
	dIdle := cur.idle - prev.idle
	if dTotal <= 0 {
		return 0
	}
	busy := dTotal - dIdle
	if busy < 0 {
		busy = 0
	}
	return (busy / dTotal) * 100
}

// sampleHostContext gathers host-wide stats once per Snapshot.
func sampleHostContext() hostContext {
	recv, sent := sampleHostNet()
	hostCPU := sampleHostCPU()
	cpuCount := logicalCPUCount()

	var memUsed, memTotal uint64
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		memTotal = vm.Total
		memUsed = vm.Used
		// Some platforms report Used=0; derive from available when needed.
		if memUsed == 0 && memTotal > 0 && vm.Available > 0 && vm.Available <= memTotal {
			memUsed = memTotal - vm.Available
		}
	}

	return hostContext{
		CPUCount:         cpuCount,
		HostCPUPercent:   hostCPU,
		MemoryUsedBytes:  memUsed,
		MemoryTotalBytes: memTotal,
		NetRecvBps:       recv,
		NetSentBps:       sent,
	}
}

// sampleProcess returns live CPU/memory/thread stats for pid.
// CPU% is the interval usage since the previous sample for this PID
// (0 on the first sample). Host fields are filled by attachHostContext.
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
		MemoryBytes: memInfo.RSS,
		Threads:     threads,
	}
}

func attachHostContext(m *ProcessMetrics, h hostContext) {
	if m == nil {
		return
	}
	m.CPUCount = h.CPUCount
	m.HostCPUPercent = h.HostCPUPercent
	m.MemoryUsedBytes = h.MemoryUsedBytes
	m.MemoryTotalBytes = h.MemoryTotalBytes
	m.NetRecvBytesPerSec = h.NetRecvBps
	m.NetSentBytesPerSec = h.NetSentBps
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
