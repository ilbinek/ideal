package server

import (
	"path/filepath"

	"arma3modmanager/internal/utils"
)

// ResolveInstallPath returns the absolute install directory for a branch.
func ResolveInstallPath(storedPath string) string {
	return utils.ResolveDataPath(storedPath)
}

// BranchInstallDir returns the canonical install path for a new branch slug.
func BranchInstallDir(slug string) string {
	return filepath.Join(utils.ResolveDataPath(BranchesBaseDir), slug)
}
