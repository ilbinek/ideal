//go:build linux

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const cgroupRoot = "/sys/fs/cgroup/ideal"

type memLimitHandle struct {
	limitMB int
	path    string
}

func createMemoryLimit(serverID, limitMB int) (*memLimitHandle, error) {
	if err := os.MkdirAll(cgroupRoot, 0755); err != nil {
		return nil, fmt.Errorf("create cgroup root %s: %w (Ideal needs write access to cgroup v2)", cgroupRoot, err)
	}
	// Ensure memory controller is available under ideal/ when possible.
	_ = os.WriteFile(filepath.Join("/sys/fs/cgroup", "cgroup.subtree_control"), []byte("+memory"), 0644)
	_ = os.WriteFile(filepath.Join(cgroupRoot, "cgroup.subtree_control"), []byte("+memory"), 0644)

	path := filepath.Join(cgroupRoot, fmt.Sprintf("server-%d", serverID))
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("create cgroup %s: %w", path, err)
	}

	bytes := uint64(limitMB) * 1024 * 1024
	if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte(strconv.FormatUint(bytes, 10)), 0644); err != nil {
		return nil, fmt.Errorf("write memory.max: %w", err)
	}

	return &memLimitHandle{limitMB: limitMB, path: path}, nil
}

func (h *memLimitHandle) attach(pid int) error {
	if h.path == "" {
		return fmt.Errorf("memory limit cgroup path is empty")
	}
	procs := filepath.Join(h.path, "cgroup.procs")
	if err := os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("move pid %d into %s: %w", pid, h.path, err)
	}
	return nil
}

func (h *memLimitHandle) close() {
	// Leave the cgroup directory; processes may still be exiting.
	// Resetting memory.max to max avoids leaving a stale hard cap if reused.
	if h.path != "" {
		_ = os.WriteFile(filepath.Join(h.path, "memory.max"), []byte("max"), 0644)
	}
}
