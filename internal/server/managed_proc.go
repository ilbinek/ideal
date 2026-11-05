package server

import (
	"os/exec"
	"sync/atomic"
	"time"
)

// managedProc is a child started by Ideal. A background Wait() reaps it so it
// cannot linger as a zombie after SIGKILL or a normal exit.
// Adopted orphans (Ideal restart) use a poller instead of cmd.Wait().
type managedProc struct {
	pid  int
	done <-chan struct{}

	code    atomic.Int32
	hasCode atomic.Bool
}

// newSpawnedProc starts reaping cmd in the background.
func newSpawnedProc(cmd *exec.Cmd) *managedProc {
	done := make(chan struct{})
	p := &managedProc{pid: cmd.Process.Pid, done: done}
	go func() {
		defer close(done)
		_ = cmd.Wait()
		if cmd.ProcessState != nil {
			p.code.Store(int32(cmd.ProcessState.ExitCode()))
			p.hasCode.Store(true)
		}
	}()
	return p
}

// exitCode returns the process's exit code. Adopted processes never have one,
// because only the parent can read it.
func (p *managedProc) exitCode() (int, bool) {
	if p == nil || !p.hasCode.Load() {
		return 0, false
	}
	return int(p.code.Load()), true
}

func (p *managedProc) exited() bool {
	if p == nil {
		return true
	}
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// wait blocks until the process has been reaped or timeout elapses.
func (p *managedProc) wait(timeout time.Duration) bool {
	if p == nil {
		return true
	}
	if timeout <= 0 {
		<-p.done
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}

// adoptProcess tracks an already-running PID (e.g. after Ideal restart).
// It cannot reap the process as a parent; it only observes exit via polling.
func adoptProcess(pid int) *managedProc {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for processRunning(pid) {
			time.Sleep(500 * time.Millisecond)
		}
	}()
	return &managedProc{pid: pid, done: done}
}
