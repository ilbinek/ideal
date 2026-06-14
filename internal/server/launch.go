package server

import (
	"fmt"
	"os"
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
	binaryName, err := ServerBinaryName(branch.InstallPath)
	if err != nil {
		return nil, err
	}
	binaryPath := filepath.Join(instanceDir, binaryName)

	args := []string{
		"-port=" + fmt.Sprint(srv.GamePort),
		"-steamPort=" + fmt.Sprint(srv.SteamPort),
		"-cfg=basic.cfg",
		"-config=server.cfg",
		"-profiles=profiles",
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

	cmdParts := append([]string{"./" + filepath.ToSlash(binaryName)}, args...)
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
		label := sm.Label
		if sm.ModID != nil {
			mod, err := database.GetMod(*sm.ModID)
			if err != nil || !mod.Downloaded || mod.LocalPath == "" {
				continue
			}
			if label == "" {
				label = mod.Name
			}
		} else if sm.CustomPath != "" {
			if _, err := os.Stat(sm.CustomPath); err != nil {
				continue
			}
			if label == "" {
				label = filepath.Base(sm.CustomPath)
			}
		} else {
			continue
		}

		linkName := uniqueLinkName(SanitizeModLabel(label), usedNames)
		if sm.IsServerMod {
			serverModsList = append(serverModsList, "servermods/"+linkName)
		} else {
			clientMods = append(clientMods, "mods/"+linkName)
		}
	}

	return strings.Join(clientMods, ";"), strings.Join(serverModsList, ";")
}
