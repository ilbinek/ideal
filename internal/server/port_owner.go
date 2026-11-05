package server

import (
	"fmt"

	"arma3modmanager/internal/database"

	gpsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// portState describes who holds a server's game port. A busy port on its own says
// nothing about the server: any process could be holding it.
type portState int

const (
	portFree portState = iota
	portOwned
	portForeign
	portOwnerUnknown
)

// classifyPort decides what a busy port means for a server.
func classifyPort(busy bool, ownerPID int, identity identityResult) portState {
	if !busy {
		return portFree
	}
	if ownerPID <= 0 || identity == identityUnknown {
		return portOwnerUnknown
	}
	if identity == identityMatch {
		return portOwned
	}
	return portForeign
}

// portUnknownLimit is how many polls an unidentifiable port holder gets before
// the server is declared dead. Our own Arma runs as our user and is always
// visible, so a holder that stays hidden isn't ours.
const portUnknownLimit = 3

// portVerdict is what the watcher should do about a server whose process is gone.
type portVerdict int

const (
	verdictDead portVerdict = iota
	verdictRetarget
	verdictHold
)

// judgePort turns a port state into a verdict, given how many consecutive polls
// the holder has been unidentifiable. It returns the updated count.
func judgePort(state portState, unknownPolls int) (portVerdict, int) {
	switch state {
	case portOwned:
		return verdictRetarget, 0
	case portOwnerUnknown:
		unknownPolls++
		if unknownPolls < portUnknownLimit {
			return verdictHold, unknownPolls
		}
		return verdictDead, unknownPolls
	default:
		return verdictDead, 0
	}
}

// findPortOwner returns the PID bound to a local port, or 0 if it can't be found.
// This walks every socket on the host, so only call it when the answer is needed.
func findPortOwner(port int) int {
	conns, err := gpsnet.Connections("inet")
	if err != nil {
		return 0
	}
	for _, c := range conns {
		if int(c.Laddr.Port) == port && c.Pid > 0 {
			return int(c.Pid)
		}
	}
	return 0
}

// inspectGamePort reports who holds the server's game port. Arma may leave a child
// holding it after the process we launched exits, so the holder is checked by its
// command line rather than the recorded PID's start time.
func inspectGamePort(srv *database.Server) (portState, int) {
	if !IsPortInUse(srv.GamePort) {
		return portFree, 0
	}
	owner := findPortOwner(srv.GamePort)
	want := serverProcessWant(srv)
	want.CreateMs = 0
	identity := identifyPID(owner, want)
	return classifyPort(true, owner, identity), owner
}

// describePortHolder names whatever holds a port, for error messages.
func describePortHolder(port int) string {
	owner := findPortOwner(port)
	if owner <= 0 {
		return ""
	}
	if p, err := process.NewProcess(int32(owner)); err == nil {
		if name, err := p.Name(); err == nil && name != "" {
			return fmt.Sprintf(" (held by %s, pid %d)", name, owner)
		}
	}
	return fmt.Sprintf(" (held by pid %d)", owner)
}

// stopOwnedPortHolder ends a leftover child of this server still holding its game
// port, which a stop of the recorded PID can't reach. Anything else is left alone.
func stopOwnedPortHolder(srv *database.Server) {
	if state, owner := inspectGamePort(srv); state == portOwned {
		_ = stopProcess(owner)
	}
}
