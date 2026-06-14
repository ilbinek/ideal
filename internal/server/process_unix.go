//go:build !windows

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
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
	return nil
}

func waitProcess(pid int) {
	for i := 0; i < 150; i++ {
		if !processRunning(pid) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func osFindProcess(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	err := syscall.Kill(pid, 0)
	return err == nil, nil
}
