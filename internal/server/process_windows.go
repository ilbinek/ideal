//go:build windows

package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
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
	return newSpawnedProc(cmd), nil
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

// killProcess ends a process tree immediately.
func killProcess(pid int) error {
	return stopProcess(pid)
}

// closeWindowTarget carries the PID into the enum callback. Callbacks from
// syscall.NewCallback are never freed, so there's one, using this shared state.
var (
	closeWindowMu     sync.Mutex
	closeWindowPID    uint32
	closeWindowPosted int

	procPostMessageW = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")

	closeWindowCallback = syscall.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &owner); err == nil && owner == closeWindowPID {
			const wmClose = 0x0010
			if ok, _, _ := procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0); ok != 0 {
				closeWindowPosted++
			}
		}
		return 1 // keep enumerating
	})
)

// closeProcess asks a server to exit by closing its window, the same as clicking
// its X. arma3server is a GUI program with no console, so console control events
// don't apply, and sending one could hit Ideal's own console instead. It reports
// false when there's no window to close, such as from another session.
func closeProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	closeWindowMu.Lock()
	defer closeWindowMu.Unlock()
	closeWindowPID = uint32(pid)
	closeWindowPosted = 0
	_ = windows.EnumWindows(closeWindowCallback, nil)
	return closeWindowPosted > 0
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
