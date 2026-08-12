package steam

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/state"
)

func testStore(t *testing.T) *state.Store {
	t.Helper()
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveToken("user", state.Token{RefreshToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEnsureSessionReconnectsDeadSession(t *testing.T) {
	st := testStore(t)
	orig := resumeLogin
	t.Cleanup(func() { resumeLogin = orig })

	live := client.UnconnectedSession()
	var calls atomic.Int32
	resumeLogin = func(ctx context.Context, opts client.Options) (*client.Session, error) {
		calls.Add(1)
		if opts.Username != "user" {
			t.Errorf("username %q", opts.Username)
		}
		return live, nil
	}

	m := &Manager{
		store: st,
		sess:  client.UnconnectedSession(),
		state: State{Phase: PhaseLoggedIn, Username: "user"},
		subs:  make(map[chan State]struct{}),
	}
	sess, err := m.EnsureSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if sess != live {
		t.Fatal("EnsureSession returned the dead session instead of reconnecting")
	}
	if calls.Load() != 1 {
		t.Fatalf("resumeLogin calls = %d, want 1", calls.Load())
	}
}

func TestEnsureSessionSerializesConcurrentResumes(t *testing.T) {
	st := testStore(t)
	orig := resumeLogin
	t.Cleanup(func() { resumeLogin = orig })

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	live := client.UnconnectedSession()
	resumeLogin = func(ctx context.Context, opts client.Options) (*client.Session, error) {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			<-release
		}
		return live, nil
	}

	m := &Manager{
		store: st,
		subs:  make(map[chan State]struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = m.EnsureSession(context.Background(), "user")
		}()
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("resumeLogin was not called")
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("concurrent resumes: calls = %d, want 1 while first is in progress", calls.Load())
	}
	close(release)
	wg.Wait()
}

func TestEnsureSessionRequiresToken(t *testing.T) {
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	orig := resumeLogin
	t.Cleanup(func() { resumeLogin = orig })
	resumeLogin = func(ctx context.Context, opts client.Options) (*client.Session, error) {
		t.Fatal("should not resume without a token")
		return nil, nil
	}
	m := &Manager{store: st, subs: make(map[chan State]struct{})}
	_, err = m.EnsureSession(context.Background(), "user")
	if err == nil {
		t.Fatal("expected error when no refresh token is stored")
	}
}
