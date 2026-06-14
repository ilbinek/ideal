package server

import "arma3modmanager/internal/utils"

// BranchInstallDir returns the SteamCMD force_install_dir target for a branch slug.
func BranchInstallDir(slug string) string {
	return utils.BranchInstallPath(slug)
}
