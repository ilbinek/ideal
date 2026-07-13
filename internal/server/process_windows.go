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

	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Kill()
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	if proc != nil {
		_ = proc.Kill()
	}
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

func osFindProcess(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	const processQueryLimited = 0x1000
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false, err
	}
	_ = syscall.CloseHandle(h)
	return true, nil
}

func processIsZombie(pid int) bool {
	return false
}
