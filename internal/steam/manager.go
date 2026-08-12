// Package steam owns the process-wide, built-in Steam connection used by the
// gosteam downloader. It holds a single authenticated session, reconnects from
// a cached refresh token when the CM connection dies, drives an interactive
// login state machine that the web UI can stream over SSE (credentials + Steam
// Guard 2FA, or QR), and persists a refresh token so future runs resume silently.
package steam

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"arma3modmanager/internal/utils"

	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/state"
)

// Login phases reported to the UI.
const (
	PhaseIdle          = "idle"
	PhaseConnecting    = "connecting"
	PhaseGuardRequired = "guard_required"
	PhaseQRChallenge   = "qr_challenge"
	PhaseLoggedIn      = "logged_in"
	PhaseError         = "error"
)

// Login methods accepted by StartLogin.
const (
	MethodCredentials = "credentials"
	MethodQR          = "qr"
)

// State is the serializable snapshot of the login state machine streamed to the
// UI.
type State struct {
	Phase        string `json:"phase"`
	Username     string `json:"username,omitempty"`
	GuardKind    string `json:"guardKind,omitempty"`    // "email" | "device"
	ChallengeURL string `json:"challengeUrl,omitempty"` // QR challenge URL to render
	Error        string `json:"error,omitempty"`
}

// Manager is the process-wide Steam session + login state machine.
type Manager struct {
	store *state.Store

	mu        sync.Mutex
	state     State
	sess      *client.Session
	loggingIn bool
	loginDone chan struct{} // closed when loggingIn becomes false; waiters must copy the ref
	guardCh   chan string
	cancel    context.CancelFunc

	subMu sync.Mutex
	subs  map[chan State]struct{}
}

// Default is the process-wide Steam manager.
var Default = &Manager{
	state: State{Phase: PhaseIdle},
	subs:  make(map[chan State]struct{}),
}

// Init opens the on-disk token/manifest store. Call once at startup, after the
// data root is known.
func (m *Manager) Init() error {
	st, err := state.Open(filepath.Join(utils.DataRoot(), ".gosteam"))
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.store = st
	m.mu.Unlock()
	return nil
}

// Store returns the underlying gosteam state store (tokens, manifest cache,
// install state). It is safe for concurrent use.
func (m *Manager) Store() *state.Store {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store
}

// Status returns the current login state.
func (m *Manager) Status() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Session returns the live session and whether one is currently established.
func (m *Manager) Session() (*client.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sess, m.sess != nil
}

// StartLogin begins an interactive login in the background. method is
// MethodCredentials (username+password, prompting for 2FA via the UI) or
// MethodQR (the UI renders a challenge URL as a QR code). It returns
// immediately; progress is observed via Status/Subscribe.
func (m *Manager) StartLogin(method, username, password string) error {
	m.mu.Lock()
	if m.store == nil {
		m.mu.Unlock()
		return errors.New("steam store not initialized")
	}
	if m.loggingIn {
		m.mu.Unlock()
		return errors.New("a login is already in progress")
	}
	if method == MethodCredentials && (username == "" || password == "") {
		m.mu.Unlock()
		return errors.New("username and password are required")
	}

	// Drop any existing session so we cleanly re-establish.
	if m.sess != nil {
		m.sess.Close()
		m.sess = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.loggingIn = true
	m.loginDone = make(chan struct{})
	m.guardCh = make(chan string, 1)
	m.state = State{Phase: PhaseConnecting, Username: username}
	store := m.store
	guardCh := m.guardCh
	m.mu.Unlock()
	m.broadcast()

	go m.runLogin(ctx, store, guardCh, method, username, password)
	return nil
}

func (m *Manager) runLogin(ctx context.Context, store *state.Store, guardCh chan string, method, username, password string) {
	holder := &sessionRef{}
	opts := client.Options{
		Username: username,
		Password: password,
		UseQR:    method == MethodQR,
		Store:    store,
		Guard: func(kind client.GuardKind) (string, error) {
			m.setState(State{Phase: PhaseGuardRequired, Username: username, GuardKind: guardKindString(kind)})
			select {
			case code := <-guardCh:
				return code, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		OnChallengeURL: func(url string) {
			m.setState(State{Phase: PhaseQRChallenge, ChallengeURL: url})
		},
		OnDisconnected: func() {
			m.onSessionDisconnected(holder.sess)
		},
	}

	sess, err := client.Login(ctx, opts)
	holder.sess = sess

	m.mu.Lock()
	m.finishLoginLocked()
	m.cancel = nil
	m.guardCh = nil
	if err != nil {
		m.sess = nil
		m.state = State{Phase: PhaseError, Username: username, Error: err.Error()}
	} else {
		m.sess = sess
		name := username
		m.state = State{Phase: PhaseLoggedIn, Username: name}
	}
	m.mu.Unlock()
	m.broadcast()
}

// SubmitGuardCode delivers a Steam Guard code to an in-progress credential
// login.
func (m *Manager) SubmitGuardCode(code string) error {
	m.mu.Lock()
	ch := m.guardCh
	phase := m.state.Phase
	m.mu.Unlock()
	if ch == nil || phase != PhaseGuardRequired {
		return errors.New("not awaiting a Steam Guard code")
	}
	select {
	case ch <- code:
		return nil
	default:
		return errors.New("a code was already submitted")
	}
}

// CancelLogin aborts an in-progress login.
func (m *Manager) CancelLogin() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
}

// Logout closes the live session. The cached refresh token is retained so a
// later EnsureSession can resume without prompting.
func (m *Manager) Logout() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	if m.sess != nil {
		m.sess.Close()
		m.sess = nil
	}
	m.state = State{Phase: PhaseIdle}
	m.mu.Unlock()
	m.broadcast()
}

// EnsureSession returns a live session, silently resuming from a cached
// refresh token for username if there is no live session (or the previous CM
// connection has died). It never prompts: if no token is cached (or the resume
// fails) it returns an error so the caller can ask the user to log in
// interactively.
func (m *Manager) EnsureSession(ctx context.Context, username string) (*client.Session, error) {
	for {
		m.mu.Lock()
		if m.sess != nil && !m.sess.Alive() {
			log.Printf("[STEAM] session dead, resuming from refresh token")
			dead := m.sess
			m.sess = nil
			m.mu.Unlock()
			dead.Close()
			continue
		}
		if m.sess != nil {
			sess := m.sess
			m.mu.Unlock()
			return sess, nil
		}
		if m.loggingIn {
			wait := m.loginDone
			m.mu.Unlock()
			if wait == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-wait:
			}
			continue
		}
		if m.store == nil {
			m.mu.Unlock()
			return nil, errors.New("steam store not initialized")
		}
		if username == "" {
			m.mu.Unlock()
			return nil, errors.New("no Steam username configured")
		}
		if _, ok := m.store.Token(username); !ok {
			m.mu.Unlock()
			return nil, errors.New("not logged in to Steam — log in from Settings")
		}
		store := m.store
		m.loggingIn = true
		m.loginDone = make(chan struct{})
		m.mu.Unlock()

		log.Printf("[STEAM] resuming session from refresh token")
		holder := &sessionRef{}
		sess, err := resumeLogin(ctx, client.Options{
			Username: username,
			Store:    store,
			OnDisconnected: func() {
				m.onSessionDisconnected(holder.sess)
			},
		})
		holder.sess = sess

		m.mu.Lock()
		m.finishLoginLocked()
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("steam session expired — log in again: %w", err)
		}
		m.sess = sess
		m.state = State{Phase: PhaseLoggedIn, Username: username}
		m.mu.Unlock()
		m.broadcast()
		return sess, nil
	}
}

// resumeLogin is client.Login; tests replace it to avoid a real Steam CM.
var resumeLogin = client.Login

type sessionRef struct {
	sess *client.Session
}

func (m *Manager) finishLoginLocked() {
	m.loggingIn = false
	if m.loginDone != nil {
		close(m.loginDone)
		m.loginDone = nil
	}
}

func (m *Manager) onSessionDisconnected(sess *client.Session) {
	if sess == nil {
		return
	}
	m.mu.Lock()
	dropped := m.sess == sess
	if dropped {
		m.sess = nil
	}
	m.mu.Unlock()
	if dropped {
		log.Printf("[STEAM] steam connection lost")
	}
}

func (m *Manager) setState(s State) {
	m.mu.Lock()
	m.state = s
	m.mu.Unlock()
	m.broadcast()
}

// Subscribe registers a new subscriber and immediately delivers the current
// state. The returned channel is closed by Unsubscribe.
func (m *Manager) Subscribe() chan State {
	ch := make(chan State, 4)
	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	ch <- m.Status()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (m *Manager) Unsubscribe(ch chan State) {
	m.subMu.Lock()
	if _, ok := m.subs[ch]; ok {
		delete(m.subs, ch)
		close(ch)
	}
	m.subMu.Unlock()
}

func (m *Manager) broadcast() {
	snap := m.Status()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}

func guardKindString(k client.GuardKind) string {
	switch k {
	case client.GuardEmailCode:
		return "email"
	case client.GuardDeviceCode:
		return "device"
	default:
		return "unknown"
	}
}
