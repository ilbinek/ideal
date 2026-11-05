package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recordedChain builds a chain whose steps note that they ran.
func recordedChain(done chan struct{}) (*stopChain, *[]string) {
	calls := &[]string{}
	note := func(s string) { *calls = append(*calls, s) }
	return &stopChain{
		done:         done,
		rconWait:     20 * time.Millisecond,
		closeWait:    20 * time.Millisecond,
		rconShutdown: func() bool { note("rcon"); return true },
		osClose:      func() bool { note("close"); return true },
		hardKill:     func(bool) error { note("kill"); return nil },
	}, calls
}

func notForced() bool { return false }

func assertCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("calls %v, want %v", got, want)
		}
	}
}

func TestChainRconExit(t *testing.T) {
	done := make(chan struct{})
	c, calls := recordedChain(done)
	c.rconShutdown = func() bool {
		*calls = append(*calls, "rcon")
		close(done) // the server goes down in response
		return true
	}
	if err := c.run(context.Background(), notForced); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "rcon")
}

func TestChainLoginFailFallsThrough(t *testing.T) {
	done := make(chan struct{})
	c, calls := recordedChain(done)
	c.rconWait = time.Hour // must not be waited on: nothing was sent
	c.rconShutdown = func() bool { *calls = append(*calls, "rcon"); return false }
	c.osClose = func() bool {
		*calls = append(*calls, "close")
		close(done)
		return true
	}
	if err := c.run(context.Background(), notForced); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "rcon", "close")
}

func TestChainEscalatesToKill(t *testing.T) {
	c, calls := recordedChain(make(chan struct{}))
	if err := c.run(context.Background(), notForced); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "rcon", "close", "kill")
}

func TestChainNoWindowSkipsWait(t *testing.T) {
	c, calls := recordedChain(make(chan struct{}))
	c.rconShutdown = nil
	c.closeWait = time.Hour
	c.osClose = func() bool { *calls = append(*calls, "close"); return false }

	finished := make(chan error, 1)
	go func() { finished <- c.run(context.Background(), notForced) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waited for a window that doesn't exist")
	}
	assertCalls(t, *calls, "close", "kill")
}

func TestChainForceInterrupts(t *testing.T) {
	c, calls := recordedChain(make(chan struct{}))
	c.rconWait = time.Hour
	c.closeWait = time.Hour
	var killedForced bool
	c.hardKill = func(forced bool) error {
		killedForced = forced
		*calls = append(*calls, "kill")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- c.run(ctx, notForced) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("force did not interrupt the wait")
	}
	assertCalls(t, *calls, "rcon", "kill")
	if !killedForced {
		t.Fatal("hard kill wasn't told it was forced")
	}
}

func TestChainForcedSkipsGracefulSteps(t *testing.T) {
	c, calls := recordedChain(make(chan struct{}))
	if err := c.run(context.Background(), func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "kill")
}

func TestChainDockerAlwaysCleansUp(t *testing.T) {
	// Exit after the graceful step: cleanup must still run, or the logs are lost
	// and the container stays behind.
	done := make(chan struct{})
	c, calls := recordedChain(done)
	c.cleanup = func() error { *calls = append(*calls, "cleanup"); return nil }
	c.rconShutdown = func() bool {
		*calls = append(*calls, "rcon")
		close(done)
		return true
	}
	if err := c.run(context.Background(), notForced); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, *calls, "rcon", "cleanup")

	// A failure during cleanup is reported rather than swallowed.
	done2 := make(chan struct{})
	close(done2)
	c2, _ := recordedChain(done2)
	wantErr := errors.New("docker gone")
	c2.cleanup = func() error { return wantErr }
	if err := c2.run(context.Background(), notForced); !errors.Is(err, wantErr) {
		t.Fatalf("got %v", err)
	}
}
