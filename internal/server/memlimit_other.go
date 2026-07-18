//go:build !windows && !linux

package server

import "fmt"

type memLimitHandle struct {
	limitMB int
}

func createMemoryLimit(serverID, limitMB int) (*memLimitHandle, error) {
	return nil, fmt.Errorf("memory limits are not supported on this platform")
}

func (h *memLimitHandle) attach(pid int) error {
	return fmt.Errorf("memory limits are not supported on this platform")
}

func (h *memLimitHandle) close() {}
