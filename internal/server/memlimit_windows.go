//go:build windows

package server

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memLimitHandle struct {
	limitMB int
	job     windows.Handle
}

func createMemoryLimit(serverID, limitMB int) (*memLimitHandle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	// Do not set KILL_ON_JOB_CLOSE — Arma must survive Ideal restarts.
	info.JobMemoryLimit = uintptr(uint64(limitMB) * 1024 * 1024)

	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}

	return &memLimitHandle{limitMB: limitMB, job: job}, nil
}

func (h *memLimitHandle) attach(pid int) error {
	// AssignProcessToJobObject requires PROCESS_SET_QUOTA | PROCESS_TERMINATE.
	access := uint32(windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE | windows.PROCESS_QUERY_INFORMATION)
	proc, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer windows.CloseHandle(proc)

	if err := windows.AssignProcessToJobObject(h.job, proc); err != nil {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}

func (h *memLimitHandle) close() {
	if h.job != 0 {
		_ = windows.CloseHandle(h.job)
		h.job = 0
	}
}
