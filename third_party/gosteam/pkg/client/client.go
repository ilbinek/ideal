// Package client provides a high-level, synchronous facade over the vendored
// go-steam Client: a single Login call that connects, authenticates (anonymous,
// stored refresh token, username/password + Steam Guard, or QR), persists the
// refresh token for next time, and returns a Session usable by the cdn/workshop
// packages. One Session is shared across an entire bulk download.
package client

import (
	"context"
	"errors"
	"fmt"

	steam "github.com/0xAozora/go-steam"
	"github.com/0xAozora/go-steam/protocol/protobuf"
	"github.com/0xAozora/go-steam/protocol/steamlang"

	"github.com/tbdevent/gosteam/pkg/state"
)

// GuardKind identifies which Steam Guard code the user must supply.
type GuardKind int

const (
	GuardUnknown GuardKind = iota
	GuardEmailCode
	GuardDeviceCode
)

func (k GuardKind) String() string {
	switch k {
	case GuardEmailCode:
		return "email code"
	case GuardDeviceCode:
		return "mobile authenticator code"
	default:
		return "guard code"
	}
}

// Options configures Login. Exactly one auth mode is selected, in priority
// order: Anonymous, then UseQR, then a stored refresh token for Username (if
// Store is set), then Username/Password credentials.
type Options struct {
	Username  string
	Password  string
	Anonymous bool
	UseQR     bool

	// Store, if set, supplies a previously saved refresh token for Username and
	// receives the refresh token after a successful credential/QR login.
	Store *state.Store

	// Guard is called when Steam requests a 2FA/email code during credential
	// login. Required for accounts with Steam Guard unless a refresh token is
	// already stored.
	Guard func(kind GuardKind) (code string, err error)

	// OnChallengeURL receives the QR challenge URL (and any rotations) during QR
	// login. If nil, the URL is printed to stdout.
	OnChallengeURL func(url string)
}

// Session is an authenticated, connected Steam client. It keeps the underlying
// connection alive (draining events and answering heartbeats) until Close.
type Session struct {
	sc      *steam.Client
	steamID uint64
	cellID  uint32
	done    chan struct{}
}

// Steam returns the underlying go-steam client for use by content packages.
func (s *Session) Steam() *steam.Client { return s.sc }

// SteamID returns the logged-in account's SteamID (0 for anonymous-as-individual edge cases).
func (s *Session) SteamID() uint64 { return s.steamID }

// CellID returns the cell id Steam assigned at logon, used to pick nearby CDN servers.
func (s *Session) CellID() uint32 { return s.cellID }

// Close logs off and disconnects.
func (s *Session) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.sc.Disconnect()
}

// Login connects to Steam, authenticates per opts, and returns a live Session.
// It blocks until logon succeeds, fails, or ctx is cancelled.
func Login(ctx context.Context, opts Options) (*Session, error) {
	sc := steam.NewClient()
	if _, err := sc.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	var refreshToken, guardData string
	if opts.Store != nil && opts.Username != "" {
		if t, ok := opts.Store.Token(opts.Username); ok {
			refreshToken, guardData = t.RefreshToken, t.GuardData
		}
	}

	for {
		select {
		case <-ctx.Done():
			sc.Disconnect()
			return nil, ctx.Err()

		case ev := <-sc.Events():
			switch e := ev.(type) {
			case *steam.ConnectedEvent:
				if err := startLogin(sc, opts, refreshToken, guardData); err != nil {
					sc.Disconnect()
					return nil, err
				}

			case *steam.LoggedOnEvent:
				if e.Result != steamlang.EResult_OK {
					sc.Disconnect()
					return nil, fmt.Errorf("logon failed: %v", e.Result)
				}
				return newSession(sc, opts, uint64(e.ClientSteamId), e.CellId), nil

			case *steam.LogOnFailedEvent:
				sc.Disconnect()
				return nil, fmt.Errorf("logon failed: %v", e.Result)

			case steam.FatalErrorEvent:
				return nil, fmt.Errorf("steam connection error: %w", error(e))
			}
		}
	}
}

func startLogin(sc *steam.Client, opts Options, refreshToken, guardData string) error {
	switch {
	case opts.Anonymous:
		return sc.Auth.LogOn(&steam.LogOnDetails{Anonymous: true})

	case opts.UseQR:
		cb := opts.OnChallengeURL
		if cb == nil {
			cb = func(u string) { fmt.Println("Scan this QR challenge URL with the Steam mobile app:", u) }
		}
		return sc.Auth.LogOnQR(&steam.LogOnDetails{}, cb)

	case refreshToken != "":
		return sc.Auth.LogOn(&steam.LogOnDetails{
			Username:               opts.Username,
			RefreshToken:           refreshToken,
			GuardData:              guardData,
			ShouldRememberPassword: true,
		})

	default:
		if opts.Username == "" || opts.Password == "" {
			return errors.New("username and password required (or use Anonymous/UseQR/a stored token)")
		}
		if opts.Guard != nil {
			sc.Auth.Authenticator = guardAuth{fn: opts.Guard}
		}
		return sc.Auth.LogOnCredentials(&steam.LogOnDetails{
			Username:               opts.Username,
			Password:               opts.Password,
			ShouldRememberPassword: true,
		})
	}
}

func newSession(sc *steam.Client, opts Options, steamID uint64, cellID uint32) *Session {
	// Persist the refresh token obtained during credential/QR login so future
	// runs need no password.
	if opts.Store != nil && !opts.Anonymous {
		if d := sc.Auth.Details; d != nil && d.RefreshToken != "" {
			user := opts.Username
			if user == "" {
				user = d.Username
			}
			if user != "" {
				_ = opts.Store.SaveToken(user, state.Token{RefreshToken: d.RefreshToken, GuardData: d.GuardData})
			}
		}
	}

	s := &Session{sc: sc, steamID: steamID, cellID: cellID, done: make(chan struct{})}
	go s.drainEvents()
	return s
}

// drainEvents keeps consuming the event channel so the read loop never blocks.
func (s *Session) drainEvents() {
	for {
		select {
		case <-s.done:
			return
		case <-s.sc.Events():
		}
	}
}

// guardAuth adapts a user Guard callback to the fork's Authenticator interface.
type guardAuth struct {
	fn func(GuardKind) (string, error)
}

func (g guardAuth) GetCode(kind protobuf.EAuthSessionGuardType, update func(string, protobuf.EAuthSessionGuardType) error) error {
	code, err := g.fn(toGuardKind(kind))
	if err != nil {
		return err
	}
	return update(code, kind)
}

func toGuardKind(k protobuf.EAuthSessionGuardType) GuardKind {
	switch k {
	case protobuf.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode:
		return GuardEmailCode
	case protobuf.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
		return GuardDeviceCode
	default:
		return GuardUnknown
	}
}
