//go:build !windows

package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return newSpawnedProc(cmd), nil
}

func stopProcess(pid int) error {
	// Kill the process group
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// gracefulCloseSignal asks Arma to shut down. Reports suggest SIGINT is its clean
// path and that SIGTERM ends it immediately, but this isn't documented, so check it
// against a real server if shutdowns misbehave.
const gracefulCloseSignal = syscall.SIGINT

// closeProcess asks a server to exit, reporting whether the signal was delivered.
func closeProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(-pid, gracefulCloseSignal); err != nil {
		return syscall.Kill(pid, gracefulCloseSignal) == nil
	}
	return true
}

// killProcess ends a process group immediately.
func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not exit after kill", pid)
}

func osFindProcess(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	err := syscall.Kill(pid, 0)
	return err == nil, nil
}

// processIsZombie reports whether pid exists only as an unreaped zombie.
func processIsZombie(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// /proc/pid/stat: pid (comm) state ...
	s := string(data)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return false
	}
	return s[i+2] == 'Z'
}
