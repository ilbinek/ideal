package server

import "time"

// managedProc is a child started by Ideal. A background Wait() reaps it so it
// cannot linger as a zombie after SIGKILL or a normal exit.
// Adopted orphans (Ideal restart) use a poller instead of cmd.Wait().
type managedProc struct {
	pid  int
	done <-chan struct{}
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
