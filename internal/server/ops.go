package server

import (
	"context"
	"fmt"
	"sync/atomic"
)

// OpKind names a lifecycle operation on a server.
type OpKind string

const (
	OpStart   OpKind = "start"
	OpStop    OpKind = "stop"
	OpRestart OpKind = "restart"
	OpCleanup OpKind = "cleanup"
	OpDelete  OpKind = "delete"
)

// Actor says who asked for an operation: a user's display name, or ActorSystem.
type Actor string

// ActorSystem marks operations Ideal performs on its own.
const ActorSystem Actor = "system"

type opPhase int32

const (
	phaseStopping opPhase = iota
	phaseStarting
)

// serverOp is the single lifecycle operation allowed on a server at a time.
//
// Rules that keep this free of deadlocks:
//   - never wait on an op while holding m.mu
//   - an op holder never waits on the watcher or on another server's op
//   - headless functions never take an op
//   - only the watcher, Stop and Force may wait for one to finish
type serverOp struct {
	kind   OpKind
	by     Actor
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	phase     atomic.Int32
	forced    atomic.Bool // jump straight to a hard kill
	skipStart atomic.Bool // a restart whose start half is no longer wanted
}

// BusyError reports that another lifecycle operation is already in progress.
type BusyError struct {
	Op OpKind
}

func (e *BusyError) Error() string {
	switch e.Op {
	case OpStart:
		return "server is starting"
	case OpStop:
		return "server is stopping"
	case OpRestart:
		return "server is restarting"
	case OpDelete:
		return "server is being deleted"
	default:
		return "server is busy"
	}
}

// tryBeginOp claims the server for an operation. When another is running, it
// returns that operation instead.
//
// intent, if given, records what a person asked for. It runs in the same critical
// section as the claim, so competing requests apply their intent in the same order
// they take effect: a Stop can't have its "stay off" overwritten by a Start that
// it then cancels.
func (m *Manager) tryBeginOp(serverID int, kind OpKind, by Actor, intent func(claimed bool)) (op, running *serverOp) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.ops == nil {
		m.ops = make(map[int]*serverOp)
	}
	if cur, ok := m.ops[serverID]; ok {
		if intent != nil {
			intent(false)
		}
		return nil, cur
	}
	if intent != nil {
		intent(true)
	}
	ctx, cancel := context.WithCancel(context.Background())
	op = &serverOp{kind: kind, by: by, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if kind == OpStart {
		op.phase.Store(int32(phaseStarting))
	}
	m.ops[serverID] = op
	return op, nil
}

// waitAndBeginOp claims the server, waiting out whatever is running first.
func (m *Manager) waitAndBeginOp(ctx context.Context, serverID int, kind OpKind, by Actor) (*serverOp, error) {
	for {
		op, running := m.tryBeginOp(serverID, kind, by, nil)
		if op != nil {
			return op, nil
		}
		select {
		case <-running.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (m *Manager) endOp(serverID int, op *serverOp) {
	m.opMu.Lock()
	if m.ops[serverID] == op {
		delete(m.ops, serverID)
	}
	m.opMu.Unlock()
	op.cancel()
	close(op.done)
}

// CurrentOp returns the operation running on a server, or "" if there is none.
func (m *Manager) CurrentOp(serverID int) OpKind {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if op, ok := m.ops[serverID]; ok {
		return op.kind
	}
	return ""
}

// BeginExclusive holds off all lifecycle operations until the returned function
// runs. Used for changes like deleting a server.
func (m *Manager) BeginExclusive(serverID int, kind OpKind, by Actor) (func(), error) {
	op, running := m.tryBeginOp(serverID, kind, by, nil)
	if op == nil {
		return nil, &BusyError{Op: running.kind}
	}
	return func() { m.endOp(serverID, op) }, nil
}

func errStartCancelled(err error) error {
	return fmt.Errorf("start cancelled: %w", err)
}
