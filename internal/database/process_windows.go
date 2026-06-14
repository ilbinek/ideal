//go:build windows

package database

import (
	"syscall"
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}
