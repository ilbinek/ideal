package server

import (
	"fmt"
	"path/filepath"
	"strings"

	"arma3modmanager/internal/database"
)

// LaunchArgs holds parsed launch parameters for preview and execution.
type LaunchArgs struct {
	Command     string   `json:"command"`
	ModParam    string   `json:"modParam"`
	ServerModParam string `json:"serverModParam"`
	Binary      string   `json:"binary"`
	Args        []string `json:"args"`
	WorkingDir  string   `json:"workingDir"`
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
	binaryRel, ok := FindServerBinary(branch.InstallPath)
	if !ok {
		return nil, fmt.Errorf("branch %q is not installed", branch.Name)
	}
	binaryName := filepath.Base(filepath.FromSlash(binaryRel))
	binaryPath := filepath.Join(instanceDir, binaryName)

	profileDir := filepath.ToSlash(filepath.Join("profiles", srv.ProfileName))
	args := []string{
		"-port=" + fmt.Sprint(srv.GamePort),
		"-steamPort=" + fmt.Sprint(srv.SteamPort),
		"-cfg=basic.cfg",
		"-config=server.cfg",
		"-profiles=" + profileDir,
		"-name=" + srv.ProfileName,
	}

	modParam, serverModParam := buildModParams(instanceDir, serverID)
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

func buildModParams(instanceDir string, serverID int) (modParam, serverModParam string) {
	serverMods, err := database.GetServerMods(serverID)
	if err != nil {
		return "", ""
	}

	usedNames := make(map[string]int)
	var clientMods, serverModsList []string

	for _, sm := range serverMods {
		linkName, _, ok := ModLinkName(sm)
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
