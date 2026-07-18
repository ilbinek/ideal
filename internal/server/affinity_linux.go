//go:build linux

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func applyAffinity(pid int, cpus []int) error {
	if len(cpus) == 0 {
		return nil
	}
	var set unix.CPUSet
	for _, c := range cpus {
		if c < 0 {
			return fmt.Errorf("invalid CPU %d", c)
		}
		set.Set(c)
	}
	if err := unix.SchedSetaffinity(pid, &set); err != nil {
		return fmt.Errorf("SchedSetaffinity: %w", err)
	}
	return nil
}

// GetCPUTopology reads HT siblings from sysfs.
func GetCPUTopology() (CPUTopology, error) {
	pairs, logical, err := linuxProcessorPairs()
	if err != nil || len(pairs) == 0 {
		n := logicalCPUCount()
		return CPUTopology{LogicalCount: n, Pairs: fallbackPairs(n)}, nil
	}
	return CPUTopology{LogicalCount: logical, Pairs: pairs}, nil
}

func linuxProcessorPairs() ([][]int, int, error) {
	entries, err := os.ReadDir("/sys/devices/system/cpu")
	if err != nil {
		return nil, 0, err
	}
	seen := make(map[string]struct{})
	var pairs [][]int
	maxCPU := -1

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		idxStr := strings.TrimPrefix(name, "cpu")
		if idxStr == "" {
			continue
		}
		if _, err := strconv.Atoi(idxStr); err != nil {
			continue
		}
		sibPath := filepath.Join("/sys/devices/system/cpu", name, "topology", "thread_siblings_list")
		data, err := os.ReadFile(sibPath)
		if err != nil {
			continue
		}
		list := strings.TrimSpace(string(data))
		if list == "" {
			continue
		}
		if _, ok := seen[list]; ok {
			continue
		}
		seen[list] = struct{}{}
		cpus, err := parseCPUList(list)
		if err != nil || len(cpus) == 0 {
			continue
		}
		for _, c := range cpus {
			if c > maxCPU {
				maxCPU = c
			}
		}
		pairs = append(pairs, cpus)
	}

	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i]) == 0 || len(pairs[j]) == 0 {
			return len(pairs[i]) < len(pairs[j])
		}
		return pairs[i][0] < pairs[j][0]
	})

	logical := maxCPU + 1
	if logical <= 0 {
		logical = logicalCPUCount()
	}
	return pairs, logical, nil
}

// parseCPUList parses kernel lists like "0-1", "0,2", "0-3,8".
func parseCPUList(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("bad range %q", part)
			}
			lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil || hi < lo {
				return nil, fmt.Errorf("bad range %q", part)
			}
			for i := lo; i <= hi; i++ {
				out = append(out, i)
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return uniqueSorted(out), nil
}
