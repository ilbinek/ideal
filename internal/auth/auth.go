package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"

	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
)

type contextKey int

const principalKey contextKey = 1

const (
	SessionCookieName = "arma3mm_session"
	NonceCookieName   = "arma3mm_steam_nonce"
	SessionDuration   = 30 * 24 * time.Hour
)

// Principal is the authenticated identity attached to a request.
type Principal struct {
	SubjectType       string // database.SubjectPasswordAdmin | database.SubjectUser
	UserID            int
	SteamID           string
	DisplayName       string
	AvatarURL         string
	IsFullAdmin       bool
	CanManageMods     bool
	CanManageBranches bool
	ServerIDs         []int
}

func (p *Principal) HasServerAccess(serverID int) bool {
	if p == nil {
		return false
	}
	if p.IsFullAdmin {
		return true
	}
	for _, id := range p.ServerIDs {
		if id == serverID {
			return true
		}
	}
	return false
}

func (p *Principal) CanAccessServers() bool {
	if p == nil {
		return false
	}
	return p.IsFullAdmin || len(p.ServerIDs) > 0
}

func (p *Principal) HasAnyAccess() bool {
	if p == nil {
		return false
	}
	return p.IsFullAdmin || p.CanManageMods || p.CanManageBranches || len(p.ServerIDs) > 0
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func cookieSecure(r *http.Request) bool {
	if config.AppConfig != nil && strings.HasPrefix(strings.ToLower(config.AppConfig.PublicURL), "https://") {
		return true
	}
	if r != nil {
		if r.TLS != nil {
			return true
		}
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
	}
	return false
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   int(SessionDuration.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   -1,
	})
}

func CreatePasswordAdminSession(w http.ResponseWriter, r *http.Request) error {
	token, err := generateToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(SessionDuration)
	if err := database.CreateSession(token, database.SubjectPasswordAdmin, nil, expires); err != nil {
		return err
	}
	setSessionCookie(w, r, token)
	return nil
}

func CreateUserSession(w http.ResponseWriter, r *http.Request, userID int) error {
	token, err := generateToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(SessionDuration)
	if err := database.CreateSession(token, database.SubjectUser, &userID, expires); err != nil {
		return err
	}
	setSessionCookie(w, r, token)
	return nil
}

func LoadPrincipal(r *http.Request) *Principal {
	return loadPrincipal(r, nil)
}

// LoadPrincipalAndRenew loads the session and, when w is non-nil, refreshes the
// cookie MaxAge alongside any sliding DB expiry update.
func LoadPrincipalAndRenew(w http.ResponseWriter, r *http.Request) *Principal {
	return loadPrincipal(r, w)
}

func loadPrincipal(r *http.Request, w http.ResponseWriter) *Principal {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	row, err := database.GetSession(cookie.Value)
	if err != nil {
		return nil
	}
	now := time.Now()
	if now.After(row.ExpiresAt) {
		_ = database.DeleteSession(cookie.Value)
		return nil
	}

	// Sliding renewal when less than half the duration remains.
	remaining := row.ExpiresAt.Sub(now)
	if remaining < SessionDuration/2 {
		newExpiry := now.Add(SessionDuration)
		if err := database.TouchSession(cookie.Value, newExpiry); err != nil {
			log.Printf("[auth] failed to renew session: %v", err)
		} else if w != nil {
			setSessionCookie(w, r, cookie.Value)
		}
	}

	p := &Principal{SubjectType: row.SubjectType}
	if row.SubjectType == database.SubjectPasswordAdmin {
		p.IsFullAdmin = true
		p.CanManageMods = true
		p.CanManageBranches = true
		p.DisplayName = "Admin"
		return p
	}

	if !row.UserID.Valid {
		return nil
	}
	u, err := database.GetUser(int(row.UserID.Int64))
	if err != nil {
		return nil
	}
	p.UserID = u.ID
	p.SteamID = u.SteamID
	p.DisplayName = u.DisplayName
	p.AvatarURL = u.AvatarURL
	p.IsFullAdmin = u.IsFullAdmin
	p.CanManageMods = u.IsFullAdmin || u.CanManageMods
	p.CanManageBranches = u.IsFullAdmin || u.CanManageBranches
	p.ServerIDs = u.ServerIDs
	if u.IsFullAdmin {
		p.CanManageMods = true
		p.CanManageBranches = true
	}
	return p
}

func ValidateSession(r *http.Request) bool {
	return LoadPrincipal(r) != nil
}

func DestroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil && cookie.Value != "" {
		_ = database.DeleteSession(cookie.Value)
	}
	clearSessionCookie(w, r)
}

func ValidatePassword(password string) bool {
	return password == config.AppConfig.Password
}

// StartSessionCleanup periodically removes expired sessions from the database.
func StartSessionCleanup() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := database.CleanupExpiredSessions(); err != nil {
				log.Printf("[auth] session cleanup: %v", err)
			}
		}
	}()
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := LoadPrincipal(r)
		if p == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(WithPrincipal(r.Context(), p)))
	}
}
