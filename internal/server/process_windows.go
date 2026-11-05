//go:build windows

package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func startProcess(binary string, args []string, workDir string, capture io.Writer) (*managedProc, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	out := io.Writer(os.Stdout)
	if capture != nil {
		// Capture first so a slow/blocked Ideal stdout cannot delay or drop
		// bytes destined for the session log file.
		out = io.MultiWriter(capture, os.Stdout)
	}
	cmd.Stdout = out
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return &managedProc{pid: cmd.Process.Pid, done: done}, nil
}

func stopProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	// Kill the whole tree — Arma can leave children holding game ports.
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	terminatePID(pid)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	terminatePID(pid)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processRunning(pid) {
		return fmt.Errorf("process %d did not exit after kill", pid)
	}
	return nil
}

// terminatePID kills pid without keeping a handle open afterwards. An open handle
// keeps a dead process's kernel object alive, which would make it look running.
func terminatePID(pid int) {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer syscall.CloseHandle(h)
	_ = syscall.TerminateProcess(h, 1)
}

// osFindProcess reports whether pid is a live process. OpenProcess alone isn't
// enough: it succeeds on an exited process while anyone still holds a handle, so
// this also checks whether the process object has been signalled.
//
// A process we aren't allowed to open reports as not running. Arma started by
// Ideal is always openable, and a protected system process that reused a stale
// PID must not block a server from starting.
func osFindProcess(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited|syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(h)
	event, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return false, err
	}
	return event == syscall.WAIT_TIMEOUT, nil
}

func processIsZombie(pid int) bool {
	return false
}
