package auth

import (
	"arma3modmanager/internal/config"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

type Session struct {
	Token     string
	ExpiresAt time.Time
}

var (
	sessions = make(map[string]*Session)
	mu       sync.RWMutex
)

const SessionCookieName = "arma3mm_session"
const SessionDuration = 24 * time.Hour

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func CreateSession(w http.ResponseWriter) error {
	token, err := generateToken()
	if err != nil {
		return err
	}

	session := &Session{
		Token:     token,
		ExpiresAt: time.Now().Add(SessionDuration),
	}

	mu.Lock()
	sessions[token] = session
	mu.Unlock()

	// Clean up expired sessions
	go cleanupExpiredSessions()

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(SessionDuration.Seconds()),
	})

	return nil
}

func ValidateSession(r *http.Request) bool {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return false
	}

	mu.RLock()
	session, exists := sessions[cookie.Value]
	mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(session.ExpiresAt) {
		mu.Lock()
		delete(sessions, cookie.Value)
		mu.Unlock()
		return false
	}

	return true
}

func DestroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil {
		mu.Lock()
		delete(sessions, cookie.Value)
		mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:   SessionCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func ValidatePassword(password string) bool {
	return password == config.AppConfig.Password
}

func cleanupExpiredSessions() {
	mu.Lock()
	defer mu.Unlock()

	now := time.Now()
	for token, session := range sessions {
		if now.After(session.ExpiresAt) {
			delete(sessions, token)
		}
	}
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ValidateSession(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
