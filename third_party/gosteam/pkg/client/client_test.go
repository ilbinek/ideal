package client

import (
	"sync"
	"testing"
	"time"
)

func TestUnconnectedSessionNotAlive(t *testing.T) {
	var nilSess *Session
	if nilSess.Alive() {
		t.Fatal("nil session should not be alive")
	}
	s := UnconnectedSession()
	if s.Alive() {
		t.Fatal("unconnected session should not be alive")
	}
	s.Close()
	if s.Alive() {
		t.Fatal("closed session should not be alive")
	}
}

func TestNotifyDisconnectedOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	done := make(chan struct{})
	s := UnconnectedSession()
	s.onDisconnected = func() {
		mu.Lock()
		calls++
		mu.Unlock()
		close(done)
	}
	s.notifyDisconnected()
	s.notifyDisconnected()
	s.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnDisconnected was not called")
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("OnDisconnected calls = %d, want 1", calls)
	}
}
