//go:build !windows && !linux

package server

import "fmt"

func applyAffinity(pid int, cpus []int) error {
	if len(cpus) == 0 {
		return nil
	}
	return fmt.Errorf("CPU affinity is not supported on this platform")
}

// GetCPUTopology falls back to consecutive logical pairs.
func GetCPUTopology() (CPUTopology, error) {
	n := logicalCPUCount()
	return CPUTopology{LogicalCount: n, Pairs: fallbackPairs(n)}, nil
}
