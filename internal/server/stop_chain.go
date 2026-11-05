package server

import (
	"context"
	"errors"
	"log"
	"time"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/rcon"
)

// How long each graceful step gets before the next one. Missions can take a while
// to wind down, so these err on the generous side; Force stop skips them.
const (
	rconStopWait = 45 * time.Second
	osCloseWait  = 30 * time.Second

	rconAttempts = 2
	rconTimeout  = time.Second // per attempt, so an RCON failure costs under 3s
)

// stopChain is the escalating shutdown: ask nicely, then close it, then kill it.
type stopChain struct {
	done <-chan struct{}

	// rconShutdown sends #shutdown, reporting false when RCON isn't available.
	rconShutdown func() bool
	rconWait     time.Duration

	// osClose asks the OS to close the process, reporting false when there's
	// nothing to deliver it to, so the wait is skipped.
	osClose   func() bool
	closeWait time.Duration

	// hardKill ends the process outright.
	hardKill func(forced bool) error

	// cleanup runs after a graceful exit. Docker needs this to save logs and remove
	// the container; native processes don't.
	cleanup func() error
}

// run works through the chain. Cancelling ctx (Force stop) jumps to the hard kill.
func (c stopChain) run(ctx context.Context, forced func() bool) error {
	if !forced() && ctx.Err() == nil && c.rconShutdown != nil && c.rconShutdown() {
		if c.waitExit(ctx, c.rconWait) {
			return c.finish()
		}
	}
	if !forced() && ctx.Err() == nil && c.osClose != nil && c.osClose() {
		if c.waitExit(ctx, c.closeWait) {
			return c.finish()
		}
	}
	return c.hardKill(forced() || ctx.Err() != nil)
}

func (c stopChain) waitExit(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (c stopChain) finish() error {
	if c.cleanup != nil {
		return c.cleanup()
	}
	return nil
}

// sendRConShutdown tries to shut a server down over RCON. It reports true if the
// command went out, including when no acknowledgement came back, since a server
// going down may never answer. Anything else falls through to the next step.
func sendRConShutdown(srv *database.Server) bool {
	_, creds, usable := loadRCon(srv)
	if !usable {
		return false
	}
	client, err := rcon.Dial(creds.Host, creds.Port, creds.Password, rconAttempts, rconTimeout)
	if err != nil {
		log.Printf("[SERVER] RCON unavailable for server %d, falling back: %v", srv.ID, err)
		return false
	}
	defer client.Close()
	if err := client.Command("#shutdown"); err != nil && !errors.Is(err, rcon.ErrNoAck) {
		log.Printf("[SERVER] RCON shutdown failed for server %d, falling back: %v", srv.ID, err)
		return false
	}
	log.Printf("[SERVER] sent RCON shutdown to server %d", srv.ID)
	return true
}

// SendRConMessage shows a message to all players. It fails when RCON isn't usable,
// which is routine on servers without BattlEye.
func SendRConMessage(serverID int, message string) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	_, creds, usable := loadRCon(srv)
	if !usable {
		return errRConUnavailable
	}
	client, err := rcon.Dial(creds.Host, creds.Port, creds.Password, rconAttempts, rconTimeout)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Command(rcon.SayAll(message))
}

var errRConUnavailable = errors.New("RCON is not available for this server")

// IsRConUnavailable reports whether an error just means the server has no RCON.
func IsRConUnavailable(err error) bool {
	return errors.Is(err, errRConUnavailable)
}

// SendRConMessage is on the Manager too, so callers can depend on an interface.
func (m *Manager) SendRConMessage(serverID int, message string) error {
	return SendRConMessage(serverID, message)
}

func chainForInstance(inst *runningInstance, srv *database.Server) stopChain {
	chain := stopChain{
		done:      inst.proc.done,
		rconWait:  rconStopWait,
		closeWait: osCloseWait,
	}
	if srv != nil {
		chain.rconShutdown = func() bool { return sendRConShutdown(srv) }
	}
	if inst.dockerMode {
		chain.osClose = func() bool { return signalDockerContainer(inst.serverID) }
		chain.hardKill = func(forced bool) error {
			wait := dockerStopSeconds
			if forced {
				wait = 0
			}
			return stopDockerContainerAfter(inst.serverID, inst.sessionLogPath, wait)
		}
		chain.cleanup = func() error {
			return stopDockerContainerAfter(inst.serverID, inst.sessionLogPath, dockerStopSeconds)
		}
		return chain
	}
	chain.osClose = func() bool { return closeProcess(inst.pid) }
	chain.hardKill = func(bool) error { return killProcess(inst.pid) }
	return chain
}

// terminateInstance ends the process behind an instance, as gently as it can.
func terminateInstance(ctx context.Context, inst *runningInstance, op *serverOp) error {
	srv, err := database.GetServer(inst.serverID)
	if err != nil {
		srv = nil
	}
	return chainForInstance(inst, srv).run(ctx, op.forced.Load)
}
