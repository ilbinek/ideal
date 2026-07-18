package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"arma3modmanager/internal/database"
)

// CPUTopology describes logical CPUs grouped into hyperthread sibling pairs.
type CPUTopology struct {
	LogicalCount int     `json:"logicalCount"`
	Pairs        [][]int `json:"pairs"`
}

// AffinityConflictError means the requested CPUs overlap another server's mask.
type AffinityConflictError struct {
	ServerName string
	CPUs       []int
}

func (e *AffinityConflictError) Error() string {
	return fmt.Sprintf("CPU affinity overlaps with server %q (cores %s)", e.ServerName, FormatAffinity(e.CPUs))
}

// ParseAffinity parses a stored affinity string ("0,1,4,5") into sorted unique indices.
func ParseAffinity(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	seen := make(map[int]struct{}, len(parts))
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// FormatAffinity stores sorted unique CPUs as a comma-separated string.
func FormatAffinity(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}
	cpus = uniqueSorted(cpus)
	parts := make([]string, len(cpus))
	for i, c := range cpus {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, ",")
}

func uniqueSorted(cpus []int) []int {
	seen := make(map[int]struct{}, len(cpus))
	out := make([]int, 0, len(cpus))
	for _, c := range cpus {
		if c < 0 {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// NormalizeAffinity expands selection to full HT sibling sets and validates pairs.
// Empty input means no pinning.
func NormalizeAffinity(cpus []int, topo CPUTopology) ([]int, error) {
	cpus = uniqueSorted(cpus)
	if len(cpus) == 0 {
		return nil, nil
	}
	if topo.LogicalCount <= 0 || len(topo.Pairs) == 0 {
		return nil, fmt.Errorf("CPU topology unavailable")
	}

	pairOf := make(map[int][]int, topo.LogicalCount)
	for _, pair := range topo.Pairs {
		p := uniqueSorted(pair)
		if len(p) == 0 {
			continue
		}
		for _, c := range p {
			pairOf[c] = p
		}
	}

	selected := make(map[int]struct{})
	for _, c := range cpus {
		pair, ok := pairOf[c]
		if !ok {
			return nil, fmt.Errorf("CPU %d is not in host topology", c)
		}
		for _, sib := range pair {
			selected[sib] = struct{}{}
		}
	}

	out := make([]int, 0, len(selected))
	for c := range selected {
		out = append(out, c)
	}
	sort.Ints(out)

	// Every selected CPU must belong to a fully selected pair.
	for _, c := range out {
		pair := pairOf[c]
		for _, sib := range pair {
			if _, ok := selected[sib]; !ok {
				return nil, fmt.Errorf("incomplete HT pair for CPU %d (need %s)", c, FormatAffinity(pair))
			}
		}
	}
	return out, nil
}

// FindAffinityConflict returns a conflict if cpus intersect another server's affinity.
// excludeServerID skips that server (for updates). Empty cpus never conflict.
func FindAffinityConflict(excludeServerID int, cpus []int) error {
	cpus = uniqueSorted(cpus)
	if len(cpus) == 0 {
		return nil
	}
	want := make(map[int]struct{}, len(cpus))
	for _, c := range cpus {
		want[c] = struct{}{}
	}

	servers, err := database.GetServers()
	if err != nil {
		return err
	}
	for _, s := range servers {
		if s.ID == excludeServerID {
			continue
		}
		other := ParseAffinity(s.CPUAffinity)
		if len(other) == 0 {
			continue
		}
		var overlap []int
		for _, c := range other {
			if _, ok := want[c]; ok {
				overlap = append(overlap, c)
			}
		}
		if len(overlap) > 0 {
			return &AffinityConflictError{ServerName: s.Name, CPUs: uniqueSorted(overlap)}
		}
	}
	return nil
}

// FindRunningAffinityConflict like FindAffinityConflict but only against running/starting servers.
func FindRunningAffinityConflict(excludeServerID int, cpus []int) error {
	cpus = uniqueSorted(cpus)
	if len(cpus) == 0 {
		return nil
	}
	want := make(map[int]struct{}, len(cpus))
	for _, c := range cpus {
		want[c] = struct{}{}
	}

	servers, err := database.GetServers()
	if err != nil {
		return err
	}
	for _, s := range servers {
		if s.ID == excludeServerID {
			continue
		}
		if s.Status != database.ServerStatusRunning && s.Status != database.ServerStatusStarting {
			continue
		}
		other := ParseAffinity(s.CPUAffinity)
		if len(other) == 0 {
			continue
		}
		var overlap []int
		for _, c := range other {
			if _, ok := want[c]; ok {
				overlap = append(overlap, c)
			}
		}
		if len(overlap) > 0 {
			return &AffinityConflictError{ServerName: s.Name, CPUs: uniqueSorted(overlap)}
		}
	}
	return nil
}

// ApplyServerAffinity pins pid to the server's configured affinity (no-op if empty).
func ApplyServerAffinity(pid int, cpuAffinity string) error {
	cpus := ParseAffinity(cpuAffinity)
	if len(cpus) == 0 || pid <= 0 {
		return nil
	}
	return applyAffinity(pid, cpus)
}

// fallbackPairs builds consecutive pairs (0,1), (2,3), … when OS topology is unavailable.
func fallbackPairs(logicalCount int) [][]int {
	if logicalCount <= 0 {
		return nil
	}
	pairs := make([][]int, 0, (logicalCount+1)/2)
	for i := 0; i < logicalCount; i += 2 {
		if i+1 < logicalCount {
			pairs = append(pairs, []int{i, i + 1})
		} else {
			pairs = append(pairs, []int{i})
		}
	}
	return pairs
}
