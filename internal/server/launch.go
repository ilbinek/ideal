package server

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"arma3modmanager/internal/database"
)

// LaunchArgs holds parsed launch parameters for preview and execution.
type LaunchArgs struct {
	Command        string   `json:"command"`
	ModParam       string   `json:"modParam"`
	ServerModParam string   `json:"serverModParam"`
	Binary         string   `json:"binary"`
	Args           []string `json:"args"`
	WorkingDir     string   `json:"workingDir"`
}

// BuildLaunchArgs constructs launch arguments for a server instance.
func BuildLaunchArgs(serverID int) (*LaunchArgs, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return nil, err
	}

	instanceDir := InstanceDir(srv.Slug)
	binaryRel, ok := ResolveServerBinary(branch.InstallPath, srv.ServerBinary)
	if !ok {
		if strings.TrimSpace(srv.ServerBinary) != "" {
			return nil, fmt.Errorf("server binary %q not found in branch %q", srv.ServerBinary, branch.Name)
		}
		return nil, fmt.Errorf("branch %q is not installed", branch.Name)
	}
	binaryName := filepath.Base(filepath.FromSlash(binaryRel))
	binaryPath := filepath.Join(instanceDir, binaryName)

	profileProfilesArg, profileNameArg := serverProfileLaunchArgs(srv.ProfileName)
	args := []string{
		"-port=" + fmt.Sprint(srv.GamePort),
		"-steamPort=" + fmt.Sprint(srv.SteamPort),
		"-cfg=basic.cfg",
		"-config=server.cfg",
		"-profiles=" + profileProfilesArg,
		"-name=" + profileNameArg,
	}

	modParam, serverModParam := buildModParams(instanceDir, serverID, srv.Slug)
	if modParam != "" {
		args = append(args, "-mod="+modParam)
	}
	if serverModParam != "" {
		args = append(args, "-serverMod="+serverModParam)
	}

	if extra := strings.TrimSpace(srv.ExtraLaunchParams); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}

	cmdParts := append([]string{"./" + filepath.ToSlash(binaryName)}, formatCommandArgs(args)...)
	return &LaunchArgs{
		Command:        strings.Join(cmdParts, " "),
		ModParam:       modParam,
		ServerModParam: serverModParam,
		Binary:         binaryPath,
		Args:           args,
		WorkingDir:     instanceDir,
	}, nil
}

// BuildHeadlessLaunchArgs constructs launch arguments for one headless client slot.
func BuildHeadlessLaunchArgs(serverID, slot int) (*LaunchArgs, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	hc, err := database.GetHeadlessClient(serverID, slot)
	if err != nil {
		return nil, fmt.Errorf("headless client slot %d not found", slot)
	}

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return nil, err
	}

	instanceDir := InstanceDir(srv.Slug)
	binaryRel, ok := ResolveServerBinary(branch.InstallPath, srv.ServerBinary)
	if !ok {
		if strings.TrimSpace(srv.ServerBinary) != "" {
			return nil, fmt.Errorf("server binary %q not found in branch %q", srv.ServerBinary, branch.Name)
		}
		return nil, fmt.Errorf("branch %q is not installed", branch.Name)
	}
	binaryName := filepath.Base(filepath.FromSlash(binaryRel))
	binaryPath := filepath.Join(instanceDir, binaryName)

	profileName := strings.TrimSpace(hc.ProfileName)
	if profileName == "" {
		profileName = database.HeadlessProfileName(srv.Name, srv.Slug, slot)
	}

	args := []string{
		"-client",
		"-connect=127.0.0.1",
		"-port=" + fmt.Sprint(srv.GamePort),
		"-profiles=profiles",
		"-name=" + profileName,
	}
	if password := ParseServerCfgPassword(srv.Slug); password != "" {
		args = append(args, "-password="+password)
	}

	modParam, _ := buildModParams(instanceDir, serverID, srv.Slug)
	if modParam != "" {
		args = append(args, "-mod="+modParam)
	}

	cmdParts := append([]string{"./" + filepath.ToSlash(binaryName)}, formatCommandArgs(args)...)
	return &LaunchArgs{
		Command:    strings.Join(cmdParts, " "),
		ModParam:   modParam,
		Binary:     binaryPath,
		Args:       args,
		WorkingDir: instanceDir,
	}, nil
}

var serverCfgPasswordRe = regexp.MustCompile(`(?im)^\s*password\s*=\s*"([^"]*)"\s*;`)

// ParseServerCfgPassword reads password = "..." from the instance server.cfg.
func ParseServerCfgPassword(slug string) string {
	content, err := ReadConfigFile(slug, "server.cfg")
	if err != nil {
		return ""
	}
	m := serverCfgPasswordRe.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func buildModParams(instanceDir string, serverID int, slug string) (modParam, serverModParam string) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return "", ""
	}
	serverMods, err := database.GetServerMods(serverID)
	if err != nil {
		return "", ""
	}

	usedNames := make(map[string]int)
	var clientMods, serverModsList []string

	fallbackGameDir := ""
	if branch, err := database.GetBranch(srv.BranchID); err == nil {
		fallbackGameDir = BranchGameDir(branch.InstallPath)
	}
	for _, code := range FilterResolvableCdlcs(srv.EnabledCdlcs, fallbackGameDir) {
		clientMods = append(clientMods, code)
	}

	for _, sm := range serverMods {
		if !sm.Enabled {
			continue
		}
		linkName, _, ok := ModLinkName(slug, sm)
		if !ok {
			continue
		}

		linkName = uniqueLinkName(linkName, usedNames)
		if sm.IsServerMod {
			serverModsList = append(serverModsList, "servermods/"+linkName)
		} else {
			clientMods = append(clientMods, "mods/"+linkName)
		}
	}

	return strings.Join(clientMods, ";"), strings.Join(serverModsList, ";")
}

// formatCommandArgs quotes mod path lists for display; exec still uses raw args.
func formatCommandArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if strings.HasPrefix(arg, "-mod=") || strings.HasPrefix(arg, "-serverMod=") {
			prefix, val, ok := strings.Cut(arg, "=")
			if ok && val != "" {
				out[i] = prefix + "=\"" + val + "\""
				continue
			}
		}
		out[i] = arg
	}
	return out
}

func serverProfileLaunchArgs(profileName string) (profilesArg, nameArg string) {
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = "Server"
	}
	return "profiles", name
}
