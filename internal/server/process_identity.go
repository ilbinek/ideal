package server

import (
	"fmt"
	"path/filepath"
	"strings"

	"arma3modmanager/internal/database"

	"github.com/shirou/gopsutil/v4/process"
)

// identityResult says whether a live PID is really the process we recorded.
// A PID alone proves nothing after a reboot: the OS may have handed it to
// something unrelated, and Stop force-kills whatever it points at.
type identityResult int

const (
	// identityGone means no process with that PID is running.
	identityGone identityResult = iota
	// identityMismatch means the PID is alive but belongs to something else.
	identityMismatch
	// identityMatch means the PID is the recorded process.
	identityMatch
	// identityUnknown means the process could not be inspected. Callers must
	// neither adopt it nor kill it.
	identityUnknown
)

// createTimeTolerance absorbs rounding between separate reads of a process's
// start time.
const createTimeToleranceMs = 2000

// processFacts is what the OS reports about a PID. Empty fields were unreadable.
type processFacts struct {
	CreateMs int64
	Name     string
	Cmdline  string
}

// processWant describes the process we expect to find.
type processWant struct {
	CreateMs int64
	Binary   string   // configured binary name; arma3server* always qualifies
	Tokens   []string // arguments the command line must contain
}

// checkIdentity decides whether got is the process described by want.
//
// A matching start time is conclusive. A differing one is not quite, because
// Linux derives it from the boot time, which moves if the clock is stepped, so
// a command line naming this server's own port still counts as a match. Any
// other process fails on the name or the arguments.
func checkIdentity(want processWant, got processFacts) identityResult {
	if want.CreateMs > 0 && got.CreateMs > 0 {
		diff := want.CreateMs - got.CreateMs
		if diff < 0 {
			diff = -diff
		}
		if diff <= createTimeToleranceMs {
			return identityMatch
		}
	}
	if got.Name == "" && got.Cmdline == "" {
		if got.CreateMs > 0 && want.CreateMs > 0 {
			return identityMismatch // start time was readable and differs
		}
		return identityUnknown
	}
	if got.Name != "" && !isServerBinaryName(got.Name, want.Binary) {
		return identityMismatch
	}
	if got.Cmdline == "" {
		if got.CreateMs > 0 && want.CreateMs > 0 {
			return identityMismatch
		}
		return identityUnknown
	}
	if !cmdlineHasTokens(got.Cmdline, want.Tokens) {
		return identityMismatch
	}
	return identityMatch
}

func isServerBinaryName(name, configured string) bool {
	name = strings.ToLower(filepath.Base(name))
	if strings.HasPrefix(name, "arma3server") {
		return true
	}
	configured = strings.ToLower(filepath.Base(filepath.FromSlash(strings.TrimSpace(configured))))
	return configured != "" && configured != "." && name == configured
}

func cmdlineHasTokens(cmdline string, tokens []string) bool {
	fields := strings.Fields(cmdline)
	for _, want := range tokens {
		found := false
		for _, f := range fields {
			if strings.EqualFold(strings.Trim(f, `"`), want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// readProcessFacts inspects a live PID. Anything unreadable stays empty.
func readProcessFacts(pid int) processFacts {
	var facts processFacts
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return facts
	}
	if ms, err := p.CreateTime(); err == nil {
		facts.CreateMs = ms
	}
	if name, err := p.Name(); err == nil {
		facts.Name = name
	}
	if cmd, err := p.Cmdline(); err == nil {
		facts.Cmdline = cmd
	}
	return facts
}

// captureCreateMs returns a PID's start time, or 0 if it can't be read. Persisted
// next to the PID so a later Ideal run can tell the process from a reused PID.
func captureCreateMs(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0
	}
	ms, err := p.CreateTime()
	if err != nil {
		return 0
	}
	return ms
}

func serverProcessWant(s *database.Server) processWant {
	return processWant{
		CreateMs: s.PIDCreateMs,
		Binary:   s.ServerBinary,
		Tokens:   []string{fmt.Sprintf("-port=%d", s.GamePort)},
	}
}

func headlessProcessWant(c *database.HeadlessClient, binary string) processWant {
	return processWant{
		CreateMs: c.PIDCreateMs,
		Binary:   binary,
		Tokens:   []string{"-client", "-name=" + c.ProfileName},
	}
}

func identifyPID(pid int, want processWant) identityResult {
	if pid <= 0 || !processRunning(pid) {
		return identityGone
	}
	return checkIdentity(want, readProcessFacts(pid))
}

// identifyServerPID reports whether the server's recorded PID is still its process.
func identifyServerPID(s *database.Server) identityResult {
	return identifyPID(s.PID, serverProcessWant(s))
}

// identifyHeadlessPID reports whether an HC slot's recorded PID is still its process.
func identifyHeadlessPID(c *database.HeadlessClient) identityResult {
	binary := ""
	if srv, err := database.GetServer(c.ServerID); err == nil {
		binary = srv.ServerBinary
	}
	return identifyPID(c.PID, headlessProcessWant(c, binary))
}
