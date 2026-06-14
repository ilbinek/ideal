package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

// ProvisionServer builds the server instance directory with symlinks and configs.
func ProvisionServer(serverID int) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return err
	}

	if !IsBranchInstalled(branch.InstallPath) {
		return fmt.Errorf("branch %q is not installed", branch.Name)
	}

	instanceDir := InstanceDir(srv.Slug)
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		return err
	}

	// Instance-owned directories
	for _, sub := range []string{"profiles", "keys", "mpmissions", "mods", "servermods"} {
		if err := os.MkdirAll(filepath.Join(instanceDir, sub), 0755); err != nil {
			return err
		}
	}

	// Default configs on first create
	serverCfgPath := filepath.Join(instanceDir, "server.cfg")
	if _, err := os.Stat(serverCfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(serverCfgPath, []byte(DefaultServerCfg(srv.Name)), 0644); err != nil {
			return err
		}
	}
	basicCfgPath := filepath.Join(instanceDir, "basic.cfg")
	if _, err := os.Stat(basicCfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(basicCfgPath, []byte(DefaultBasicCfg()), 0644); err != nil {
			return err
		}
	}

	// Symlink branch game files into instance (from steamapps/common/... when applicable)
	gameDir := BranchGameDir(branch.InstallPath)
	entries, err := os.ReadDir(gameDir)
	if err != nil {
		return fmt.Errorf("failed to read branch install at %s: %w", gameDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if instanceSkipEntries[name] {
			continue
		}
		src := filepath.Join(gameDir, name)
		dst := filepath.Join(instanceDir, name)
		if err := CreateSymlink(src, dst); err != nil {
			log.Printf("[PROVISION] warning: failed to symlink %s: %v", name, err)
		}
	}

	// Rebuild mod symlinks
	if err := provisionMods(instanceDir, serverID); err != nil {
		return err
	}

	// Regenerate keys
	return provisionKeys(instanceDir, serverID)
}

func provisionMods(instanceDir string, serverID int) error {
	modsDir := filepath.Join(instanceDir, "mods")
	serverModsDir := filepath.Join(instanceDir, "servermods")
	if err := RemoveDirContents(modsDir); err != nil {
		return err
	}
	if err := RemoveDirContents(serverModsDir); err != nil {
		return err
	}

	serverMods, err := database.GetServerMods(serverID)
	if err != nil {
		return err
	}

	usedNames := make(map[string]int)
	for _, sm := range serverMods {
		var srcPath string
		label := sm.Label

		if sm.ModID != nil {
			mod, err := database.GetMod(*sm.ModID)
			if err != nil || !mod.Downloaded || mod.LocalPath == "" {
				log.Printf("[PROVISION] skipping mod %d: not downloaded", *sm.ModID)
				continue
			}
			srcPath = mod.LocalPath
			if label == "" {
				label = mod.Name
			}
		} else if sm.CustomPath != "" {
			if err := ValidateCustomModPath(sm.CustomPath); err != nil {
				log.Printf("[PROVISION] skipping custom mod %s: %v", sm.CustomPath, err)
				continue
			}
			srcPath = sm.CustomPath
			if label == "" {
				label = filepath.Base(sm.CustomPath)
			}
		} else {
			continue
		}

		linkName := uniqueLinkName(SanitizeModLabel(label), usedNames)
		destDir := modsDir
		if sm.IsServerMod {
			destDir = serverModsDir
		}
		if err := CreateSymlink(srcPath, filepath.Join(destDir, linkName)); err != nil {
			return fmt.Errorf("failed to symlink mod %s: %w", label, err)
		}
	}
	return nil
}

func uniqueLinkName(base string, used map[string]int) string {
	if _, ok := used[base]; !ok {
		used[base] = 1
		return base
	}
	used[base]++
	return fmt.Sprintf("%s-%d", base, used[base])
}

func provisionKeys(instanceDir string, serverID int) error {
	keysDir := filepath.Join(instanceDir, "keys")
	if err := RemoveDirContents(keysDir); err != nil {
		return err
	}
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}

	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return err
	}
	branchKeysDir := filepath.Join(BranchGameDir(branch.InstallPath), "keys")
	if err := utils.CopyBikeysFromDir(branchKeysDir, keysDir); err != nil {
		log.Printf("[PROVISION] warning: failed to copy branch keys from %s: %v", branchKeysDir, err)
	}

	serverMods, err := database.GetServerMods(serverID)
	if err != nil {
		return err
	}

	var modPaths []string
	for _, sm := range serverMods {
		if sm.ModID != nil {
			mod, err := database.GetMod(*sm.ModID)
			if err == nil && mod.Downloaded && mod.LocalPath != "" {
				modPaths = append(modPaths, mod.LocalPath)
			}
		} else if sm.CustomPath != "" {
			modPaths = append(modPaths, sm.CustomPath)
		}
	}

	if err := utils.ExtractKeys(modPaths, keysDir); err != nil {
		return err
	}

	customKeys, err := database.GetServerKeys(serverID)
	if err != nil {
		return err
	}
	for _, k := range customKeys {
		dest := filepath.Join(keysDir, k.Filename)
		if err := utils.CopyFile(k.SourcePath, dest); err != nil {
			log.Printf("[PROVISION] warning: failed to copy key %s: %v", k.Filename, err)
		}
	}
	return nil
}
