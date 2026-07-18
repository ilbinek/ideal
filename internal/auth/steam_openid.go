package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"arma3modmanager/internal/config"
	"arma3modmanager/internal/database"
)

const steamOpenIDEndpoint = "https://steamcommunity.com/openid/login"

var steamIDFromClaimed = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d+)$`)

// PublicBaseURL returns the absolute site origin for OpenID realm/return_to.
func PublicBaseURL(r *http.Request) string {
	if config.AppConfig != nil {
		if u := strings.TrimRight(strings.TrimSpace(config.AppConfig.PublicURL), "/"); u != "" {
			return u
		}
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.Split(fwd, ",")[0]
		host = strings.TrimSpace(host)
	}
	return scheme + "://" + host
}

func SteamLoginEnabled() bool {
	return config.AppConfig != nil && config.AppConfig.SteamLoginEnabled
}

// SteamOpenIDStartHandler redirects the browser to Steam OpenID.
func SteamOpenIDStartHandler(w http.ResponseWriter, r *http.Request) {
	if !SteamLoginEnabled() {
		http.Error(w, "Steam login is disabled", http.StatusNotFound)
		return
	}

	nonce, err := generateToken()
	if err != nil {
		http.Error(w, "failed to start Steam login", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     NonceCookieName,
		Value:    nonce,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   int((10 * time.Minute).Seconds()),
	})

	base := PublicBaseURL(r)
	returnTo := base + "/api/auth/steam/callback?nonce=" + url.QueryEscape(nonce)

	params := url.Values{}
	params.Set("openid.ns", "http://specs.openid.net/auth/2.0")
	params.Set("openid.mode", "checkid_setup")
	params.Set("openid.return_to", returnTo)
	params.Set("openid.realm", base+"/")
	params.Set("openid.identity", "http://specs.openid.net/auth/2.0/identifier_select")
	params.Set("openid.claimed_id", "http://specs.openid.net/auth/2.0/identifier_select")

	http.Redirect(w, r, steamOpenIDEndpoint+"?"+params.Encode(), http.StatusFound)
}

// SteamOpenIDCallbackHandler verifies Steam's assertion and creates a user session.
func SteamOpenIDCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if !SteamLoginEnabled() {
		http.Error(w, "Steam login is disabled", http.StatusNotFound)
		return
	}

	nonceCookie, err := r.Cookie(NonceCookieName)
	if err != nil || nonceCookie.Value == "" {
		http.Error(w, "missing Steam login state", http.StatusBadRequest)
		return
	}
	qNonce := r.URL.Query().Get("nonce")
	if qNonce == "" || qNonce != nonceCookie.Value {
		http.Error(w, "invalid Steam login state", http.StatusBadRequest)
		return
	}

	// Clear nonce cookie
	http.SetCookie(w, &http.Cookie{
		Name:     NonceCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
		MaxAge:   -1,
	})

	if r.URL.Query().Get("openid.mode") == "cancel" {
		http.Redirect(w, r, "/login?error=steam_cancelled", http.StatusFound)
		return
	}

	steamID, err := verifySteamOpenID(r)
	if err != nil {
		log.Printf("[auth] Steam OpenID verify failed: %v", err)
		http.Redirect(w, r, "/login?error=steam_failed", http.StatusFound)
		return
	}

	displayName, avatarURL := fetchSteamPersona(steamID)
	if displayName == "" {
		displayName = steamID
	}

	user, err := database.UpsertSteamUser(steamID, displayName, avatarURL)
	if err != nil {
		log.Printf("[auth] upsert Steam user: %v", err)
		http.Redirect(w, r, "/login?error=steam_failed", http.StatusFound)
		return
	}

	if err := CreateUserSession(w, r, user.ID); err != nil {
		log.Printf("[auth] create Steam session: %v", err)
		http.Redirect(w, r, "/login?error=steam_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func verifySteamOpenID(r *http.Request) (string, error) {
	q := r.URL.Query()
	claimedID := q.Get("openid.claimed_id")
	m := steamIDFromClaimed.FindStringSubmatch(claimedID)
	if len(m) != 2 {
		return "", fmt.Errorf("invalid claimed_id: %s", claimedID)
	}
	steamID := m[1]

	params := url.Values{}
	for key, values := range q {
		if !strings.HasPrefix(key, "openid.") {
			continue
		}
		for _, v := range values {
			params.Add(key, v)
		}
	}
	params.Set("openid.mode", "check_authentication")

	resp, err := http.PostForm(steamOpenIDEndpoint, params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(body), "is_valid:true") {
		return "", fmt.Errorf("Steam did not validate assertion")
	}
	return steamID, nil
}

func fetchSteamPersona(steamID string) (displayName, avatarURL string) {
	return FetchSteamPersona(steamID)
}

// FetchSteamPersona looks up display name and avatar via the Steam Web API when a key is configured.
func FetchSteamPersona(steamID string) (displayName, avatarURL string) {
	cfg, err := database.GetConfig()
	if err != nil || cfg == nil || cfg.ApiKey == "" {
		return "", ""
	}

	u := fmt.Sprintf(
		"https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?key=%s&steamids=%s",
		url.QueryEscape(cfg.ApiKey), url.QueryEscape(steamID),
	)
	resp, err := http.Get(u)
	if err != nil {
		log.Printf("[auth] Steam persona fetch: %v", err)
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	var parsed struct {
		Response struct {
			Players []struct {
				PersonaName string `json:"personaname"`
				AvatarFull  string `json:"avatarfull"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", ""
	}
	if len(parsed.Response.Players) == 0 {
		return "", ""
	}
	p := parsed.Response.Players[0]
	return p.PersonaName, p.AvatarFull
}
