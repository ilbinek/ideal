//go:build windows

package server

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessRunningFalseAfterExit(t *testing.T) {
	if !processRunning(os.Getpid()) {
		t.Fatal("own process should be running")
	}
	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if processRunning(cmd.Process.Pid) {
		t.Fatal("exited process reported as running")
	}
}

// A stop used to hold an os.FindProcess handle, which kept OpenProcess working
// on the dead process, so every stop waited out its full timeout and then failed.
func TestStopProcessReturnsPromptly(t *testing.T) {
	proc, err := startProcess("ping", []string{"-n", "60", "127.0.0.1"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := stopProcess(proc.pid); err != nil {
		t.Fatalf("stopProcess: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("stop took %s", elapsed)
	}
	if processRunning(proc.pid) {
		t.Fatal("process still running after stop")
	}
	if !proc.wait(5 * time.Second) {
		t.Fatal("process not reaped")
	}
}
