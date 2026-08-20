package contentver

import (
	"fmt"
	"log"
	"os"

	"arma3modmanager/internal/database"
)

// PrepareBranchInstall sets up the next branch generation directory for an install/update.
// Returns the absolute content directory and generation number.
func PrepareBranchInstall(branchID int, installPath string) (contentDir string, generation int, err error) {
	if err := EnsureBranchMigrated(branchID, installPath); err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(BranchRoot(installPath), 0755); err != nil {
		return "", 0, err
	}

	cur, err := database.GetCurrentBranchGeneration(branchID)
	if err != nil {
		return "", 0, err
	}

	if cur == nil {
		generation = 1
		contentDir = BranchGenerationDir(installPath, generation)
		if err := os.MkdirAll(contentDir, 0755); err != nil {
			return "", 0, err
		}
	} else {
		max, err := database.MaxBranchGeneration(branchID)
		if err != nil {
			return "", 0, err
		}
		generation = max + 1
		src := BranchGenerationDir(installPath, cur.Generation)
		contentDir = BranchGenerationDir(installPath, generation)
		log.Printf("[CONTENTVER] cloning branch %d gen %d -> %d", branchID, cur.Generation, generation)
		if err := HardlinkClone(src, contentDir); err != nil {
			_ = os.RemoveAll(contentDir)
			return "", 0, fmt.Errorf("clone branch generation: %w", err)
		}
	}

	rel := BranchGenerationRelPath(installPath, generation)
	if err := database.UpsertBranchGeneration(branchID, generation, rel, "", false); err != nil {
		return "", 0, err
	}
	ProtectBranchGeneration(branchID, generation)
	return contentDir, generation, nil
}

// AbortBranchInstall removes a failed non-current generation.
func AbortBranchInstall(branchID int, installPath string, generation int) {
	defer UnprotectBranchGeneration(branchID, generation)
	cur, err := database.GetCurrentBranchGeneration(branchID)
	if err != nil || (cur != nil && cur.Generation == generation) {
		return
	}
	_ = os.RemoveAll(BranchGenerationDir(installPath, generation))
	_ = database.DeleteBranchGeneration(branchID, generation)
}

// FinalizeBranchInstall marks the generation current and updates the branch row.
func FinalizeBranchInstall(branchID int, installPath string, generation int, buildID string) error {
	defer UnprotectBranchGeneration(branchID, generation)
	contentDir := BranchGenerationDir(installPath, generation)
	if _, err := os.Stat(contentDir); err != nil {
		return fmt.Errorf("branch generation missing after install: %w", err)
	}

	rel := BranchGenerationRelPath(installPath, generation)
	if err := database.UpsertBranchGeneration(branchID, generation, rel, buildID, true); err != nil {
		return err
	}
	if err := database.SetBranchInstalled(branchID, true, buildID); err != nil {
		return err
	}
	if err := database.UpdateBranchChecked(branchID, false); err != nil {
		return err
	}
	GCBranchOrphans(branchID, installPath)
	return nil
}

// RemoveAllBranchContent deletes generation DB rows (caller removes install root on disk).
func RemoveAllBranchContent(branchID int) error {
	return database.DeleteAllBranchGenerations(branchID)
}
