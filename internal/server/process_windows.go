//go:build windows

package server

import (
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func startProcess(binary string, args []string, workDir string, capture io.Writer) (int, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	out := io.Writer(os.Stdout)
	if capture != nil {
		out = io.MultiWriter(os.Stdout, capture)
	}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

func stopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Kill()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return proc.Kill()
}

func waitProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_, _ = proc.Wait()
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
