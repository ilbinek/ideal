package server

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"arma3modmanager/internal/database"
)

// RConStatus is what the API may say about a server's RCON setup. It deliberately
// has no password field: the password is never returned by any endpoint.
type RConStatus struct {
	Configured      bool   `json:"configured"`
	Port            int    `json:"port"`
	Source          string `json:"source"`
	BattleyeEnabled bool   `json:"battleyeEnabled"`
	RestrictRcon    string `json:"restrictRcon"` // "0", "1", or "" when not set
}

// rconCreds is RCON access for a server. Never log it.
type rconCreds struct {
	Host     string
	Port     int
	Password string
}

// generatedRConIP limits generated configs to local connections, since Ideal runs
// on the same host. Drop it if real-server testing shows it blocks RCON.
const generatedRConIP = "127.0.0.1"

const (
	rconPasswordLength = 24
	rconPortSearchFrom = 10 // offsets above the game port
	rconPortSearchTo   = 49
)

var battleyeSettingRe = regexp.MustCompile(`(?im)^[ \t]*BattlEye[ \t]*=[ \t]*(\d+)`)

// battleyeEnabledInCfg reports whether server.cfg leaves BattlEye on. Only an
// explicit 0 turns it off.
func battleyeEnabledInCfg(serverCfg string) bool {
	m := battleyeSettingRe.FindStringSubmatch(serverCfg)
	return m == nil || m[1] != "0"
}

// customBEPath returns the -bepath= launch parameter, if any.
func customBEPath(extraLaunchParams string) string {
	for _, f := range strings.Fields(extraLaunchParams) {
		if len(f) > 8 && strings.EqualFold(f[:8], "-bepath=") {
			return strings.Trim(f[8:], `"`)
		}
	}
	return ""
}

// battleyeConfigDir finds where this server's RCON config lives: a custom -bepath,
// or profiles/BattlEye inside the instance. The instance's root battleye entry is
// a link shared by every server on the branch and must never be used.
func battleyeConfigDir(instanceDir, extraLaunchParams string) string {
	if custom := customBEPath(extraLaunchParams); custom != "" {
		if filepath.IsAbs(custom) {
			return custom
		}
		return filepath.Join(instanceDir, custom)
	}
	profiles := filepath.Join(instanceDir, "profiles")
	if entries, err := os.ReadDir(profiles); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), "battleye") {
				return filepath.Join(profiles, e.Name())
			}
		}
	}
	return filepath.Join(profiles, "BattlEye")
}

// beConfigStem is the config's base name, which follows the server binary.
func beConfigStem(binaryName string) string {
	if strings.Contains(strings.ToLower(binaryName), "x64") {
		return "beserver_x64"
	}
	return "beserver"
}

// findBEConfig returns the newest of the base config and BattlEye's _active_
// copies. BattlEye moves the base config aside while running and may leave only an
// active copy behind after a crash.
func findBEConfig(dir, stem string) (path string, isBase bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var newest int64
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if e.IsDir() || !strings.HasSuffix(name, ".cfg") {
			continue
		}
		base := name == stem+".cfg"
		if !base && !strings.HasPrefix(name, stem+"_active_") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime().UnixNano()
		// Prefer the base file on a tie: it's the one admins edit.
		if path == "" || mod > newest || (mod == newest && base) {
			path, isBase, newest = filepath.Join(dir, e.Name()), base, mod
		}
	}
	return path, isBase
}

// beConfig holds the settings Ideal cares about. Keys are lower-cased.
type beConfig map[string]string

func parseBEConfig(content string) beConfig {
	cfg := beConfig{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			key, value, ok = strings.Cut(line, "\t")
		}
		if !ok {
			continue
		}
		cfg[strings.ToLower(key)] = strings.TrimSpace(value)
	}
	return cfg
}

func credsFromConfig(cfg beConfig) (rconCreds, bool) {
	password := cfg["rconpassword"]
	port, err := strconv.Atoi(cfg["rconport"])
	if password == "" || err != nil || port <= 0 || port > 65535 {
		return rconCreds{}, false
	}
	host := cfg["rconip"]
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return rconCreds{Host: host, Port: port, Password: password}, true
}

// serverBinaryName resolves the binary this server launches, falling back to the
// configured value when the branch can't be inspected.
func serverBinaryName(srv *database.Server) string {
	if branch, err := database.GetBranch(srv.BranchID); err == nil {
		if contentDir, err := BranchContentDirForServer(srv.ID, branch); err == nil {
			if rel, ok := ResolveServerBinary(contentDir, srv.ServerBinary); ok {
				return filepath.Base(filepath.FromSlash(rel))
			}
		}
	}
	if name := strings.TrimSpace(srv.ServerBinary); name != "" {
		return filepath.Base(filepath.FromSlash(name))
	}
	return "arma3server_x64"
}

func serverBEPaths(srv *database.Server) (dir, stem string) {
	return battleyeConfigDir(InstanceDir(srv.Slug), srv.ExtraLaunchParams), beConfigStem(serverBinaryName(srv))
}

// serverUsesBattleye reads the server.cfg BattlEye setting.
func serverUsesBattleye(srv *database.Server) bool {
	content, err := ReadConfigFile(srv.Slug, "server.cfg")
	if err != nil {
		return true
	}
	return battleyeEnabledInCfg(content)
}

// loadRCon reads a server's RCON setup. usable is false when BattlEye is off or
// the config is incomplete, in which case RCON shouldn't be tried at all.
func loadRCon(srv *database.Server) (status RConStatus, creds rconCreds, usable bool) {
	status.BattleyeEnabled = serverUsesBattleye(srv)
	dir, stem := serverBEPaths(srv)
	path, _ := findBEConfig(dir, stem)
	if path == "" {
		return status, rconCreds{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return status, rconCreds{}, false
	}
	cfg := parseBEConfig(string(data))
	status.Source = filepath.Base(path)
	status.RestrictRcon = cfg["restrictrcon"]
	creds, ok := credsFromConfig(cfg)
	if !ok {
		return status, rconCreds{}, false
	}
	status.Configured = true
	status.Port = creds.Port
	return status, creds, status.BattleyeEnabled
}

// GetRConStatus returns a server's RCON setup, without the password.
func GetRConStatus(serverID int) (RConStatus, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return RConStatus{}, err
	}
	status, _, _ := loadRCon(srv)
	return status, nil
}

// restoreBEConfig puts the base config back if only an active copy survived, as
// happens after a crash. Without it BattlEye would start with no RCON at all.
func restoreBEConfig(srv *database.Server) error {
	dir, stem := serverBEPaths(srv)
	path, isBase := findBEConfig(dir, stem)
	if path == "" || isBase {
		return nil
	}
	if basePathExists(dir, stem) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stem+".cfg"), data, 0600)
}

func basePathExists(dir, stem string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), stem+".cfg") {
			return true
		}
	}
	return false
}

// RConGenerateResult reports what generating RCON settings did.
type RConGenerateResult struct {
	Status          RConStatus `json:"status"`
	RequiresRestart bool       `json:"requiresRestart"`
}

// GenerateRConConfig fills in whatever RCON settings are missing, never touching
// values already there. A running server only picks changes up after a restart.
func GenerateRConConfig(serverID int) (*RConGenerateResult, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	dir, stem := serverBEPaths(srv)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create BattlEye folder: %w", err)
	}

	existing := ""
	if path, _ := findBEConfig(dir, stem); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		existing = string(data)
	}
	cfg := parseBEConfig(existing)

	password := ""
	if cfg["rconpassword"] == "" {
		if password, err = randomRConPassword(); err != nil {
			return nil, err
		}
	}
	port := 0
	if _, err := strconv.Atoi(cfg["rconport"]); err != nil {
		if port, err = pickRConPort(srv); err != nil {
			return nil, err
		}
	}
	updated := appendBESettings(existing, cfg, password, port)

	target := filepath.Join(dir, stem+".cfg")
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(e.Name(), stem+".cfg") {
				target = filepath.Join(dir, e.Name())
				break
			}
		}
	}
	if err := os.WriteFile(target, []byte(updated), 0600); err != nil {
		return nil, fmt.Errorf("write %s: %w", filepath.Base(target), err)
	}
	// WriteFile keeps an existing file's mode, so tighten that too. Best effort:
	// Windows ignores this.
	_ = os.Chmod(target, 0600)

	status, _, _ := loadRCon(srv)
	return &RConGenerateResult{
		Status:          status,
		RequiresRestart: srv.Status == database.ServerStatusRunning || srv.Status == database.ServerStatusStarting,
	}, nil
}

// appendBESettings adds only the settings cfg lacks. An empty password or zero port
// means that value already exists.
func appendBESettings(existing string, cfg beConfig, password string, port int) string {
	out := existing
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if password != "" {
		out += "RConPassword " + password + "\n"
	}
	if port != 0 {
		out += "RConPort " + strconv.Itoa(port) + "\n"
	}
	if _, ok := cfg["rconip"]; !ok {
		out += "RConIP " + generatedRConIP + "\n"
	}
	if _, ok := cfg["restrictrcon"]; !ok {
		out += "RestrictRCon 0\n"
	}
	return out
}

func randomRConPassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	for i := 0; i < rconPasswordLength; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("generate RCON password: %w", err)
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String(), nil
}

// pickRConPort finds a free port near the game port, clear of every server's game
// ports and every other server's RCON port. It skips game port +4, the port
// BattlEye conventionally takes for itself.
func pickRConPort(srv *database.Server) (int, error) {
	servers, err := database.GetServers()
	if err != nil {
		return 0, err
	}
	used := map[int]bool{srv.GamePort + 4: true}
	for i := range servers {
		s := &servers[i]
		for _, p := range serverPorts(s.GamePort, s.SteamPort) {
			used[p] = true
		}
		if s.ID == srv.ID {
			continue
		}
		dir, stem := serverBEPaths(s)
		if path, _ := findBEConfig(dir, stem); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				if p, err := strconv.Atoi(parseBEConfig(string(data))["rconport"]); err == nil {
					used[p] = true
				}
			}
		}
	}
	for off := rconPortSearchFrom; off <= rconPortSearchTo; off++ {
		p := srv.GamePort + off
		if p > 65535 || used[p] || !isUDPPortFreeOnHost(p) {
			continue
		}
		return p, nil
	}
	return 0, fmt.Errorf("no free RCON port between %d and %d", srv.GamePort+rconPortSearchFrom, srv.GamePort+rconPortSearchTo)
}

func isUDPPortFreeOnHost(port int) bool {
	return isUDPPortAvailable(port)
}
