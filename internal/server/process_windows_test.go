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

// ping has no window, which is the "nothing to close" case the stop chain skips.
func TestCloseProcessNoWindow(t *testing.T) {
	proc, err := startProcess("ping", []string{"-n", "60", "127.0.0.1"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stopProcess(proc.pid) }()
	if closeProcess(proc.pid) {
		t.Fatal("reported closing a window on a process that has none")
	}
	if closeProcess(0) {
		t.Fatal("pid 0 should have nothing to close")
	}
	if !processRunning(proc.pid) {
		t.Fatal("process should be untouched")
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

// TestCloseProcessClosesRealWindow opens an actual window, so it's opt-in: run it
// with IDEAL_TEST_GUI=1. This is the only test that exercises WM_CLOSE end to end.
func TestCloseProcessClosesRealWindow(t *testing.T) {
	if os.Getenv("IDEAL_TEST_GUI") == "" {
		t.Skip("set IDEAL_TEST_GUI=1 to run; opens a window on the desktop")
	}
	script := `Add-Type -AssemblyName System.Windows.Forms; [Windows.Forms.Application]::Run((New-Object Windows.Forms.Form))`
	proc, err := startProcess("powershell", []string{"-NoProfile", "-Command", script}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stopProcess(proc.pid) }()

	deadline := time.Now().Add(20 * time.Second)
	posted := false
	for time.Now().Before(deadline) && !posted {
		posted = closeProcess(proc.pid)
		time.Sleep(200 * time.Millisecond)
	}
	if !posted {
		t.Fatal("window never appeared")
	}
	if !proc.wait(10 * time.Second) {
		t.Fatal("process did not exit after its window was closed")
	}
	if code, ok := proc.exitCode(); !ok || code != 0 {
		t.Fatalf("exit code %d (known: %v), expected a clean exit rather than a kill", code, ok)
	}
}
