package contentver

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

// PrepareModDownload sets up the next generation directory and a staging symlink
// so downloaders writing to StagingRoot()/workshopID land in the new gen.
// Returns the generation number being written.
func PrepareModDownload(modID int, workshopID string) (generation int, err error) {
	if err := EnsureModMigrated(modID, workshopID); err != nil {
		return 0, err
	}

	cur, err := database.GetCurrentModGeneration(modID)
	if err != nil {
		return 0, err
	}

	if cur == nil {
		generation = 1
		genDir := ModGenerationDir(workshopID, generation)
		if err := os.MkdirAll(genDir, 0755); err != nil {
			return 0, err
		}
	} else {
		max, err := database.MaxModGeneration(modID)
		if err != nil {
			return 0, err
		}
		generation = max + 1
		src := ModGenerationDir(workshopID, cur.Generation)
		dst := ModGenerationDir(workshopID, generation)
		log.Printf("[CONTENTVER] cloning mod %s gen %d -> %d", workshopID, cur.Generation, generation)
		if err := HardlinkClone(src, dst); err != nil {
			_ = os.RemoveAll(dst)
			return 0, fmt.Errorf("clone mod generation: %w", err)
		}
	}

	rel := RelativeGenerationPath(database.ContentKindMod, workshopID, generation)
	if err := database.UpsertModGeneration(modID, generation, rel, false); err != nil {
		return 0, err
	}
	if err := LinkStagingTarget(workshopID, ModGenerationDir(workshopID, generation)); err != nil {
		return 0, fmt.Errorf("staging symlink: %w", err)
	}
	return generation, nil
}

// AbortModDownload removes a failed non-current generation and clears staging.
func AbortModDownload(modID int, workshopID string, generation int) {
	ClearStaging(workshopID)
	cur, err := database.GetCurrentModGeneration(modID)
	if err != nil || (cur != nil && cur.Generation == generation) {
		return
	}
	_ = os.RemoveAll(ModGenerationDir(workshopID, generation))
	_ = os.RemoveAll(AddonsGenerationDir(workshopID, generation))
	_ = database.DeleteModGeneration(modID, generation)
}

// FinalizeModDownload marks the generation current, rebuilds its addons mirror,
// updates the mods row, and GCs unreferenced older gens.
//
// gosteam writes through a staging symlink into the generation dir.
// SteamCMD / DepotDownloader RemoveAll the staging path and recreate it as a
// real directory — in that case we replace the generation dir with staging.
func FinalizeModDownload(modID int, workshopID string, generation int) error {
	defer ClearStaging(workshopID)

	genDir := ModGenerationDir(workshopID, generation)
	staging := filepath.Join(StagingRoot(), workshopID)

	if info, err := os.Lstat(staging); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// Content already landed in genDir via the symlink.
		} else if info.IsDir() {
			_ = os.RemoveAll(genDir)
			if err := os.Rename(staging, genDir); err != nil {
				if err := HardlinkClone(staging, genDir); err != nil {
					return fmt.Errorf("move staging into generation: %w", err)
				}
				_ = os.RemoveAll(staging)
			}
		}
	}

	if _, err := os.Stat(genDir); err != nil {
		return fmt.Errorf("mod generation missing after download: %w", err)
	}

	addons := AddonsGenerationDir(workshopID, generation)
	_ = os.RemoveAll(addons)
	if err := utils.SyncModMirror(genDir, addons); err != nil {
		return fmt.Errorf("sync addons mirror: %w", err)
	}

	rel := RelativeGenerationPath(database.ContentKindMod, workshopID, generation)
	if err := database.UpsertModGeneration(modID, generation, rel, true); err != nil {
		return err
	}
	if err := database.UpdateModDownloaded(modID, true, ModLogicalPath(workshopID)); err != nil {
		return err
	}

	GCModOrphans(modID, workshopID)
	return nil
}

// RemoveAllModContent deletes every generation (disk + DB) for a mod.
func RemoveAllModContent(modID int, workshopID string) error {
	ClearStaging(workshopID)
	root := ModRoot(workshopID)
	addonsRoot := filepath.Join(utils.AddonsRoot(), strings.ToLower(workshopID))
	if err := os.RemoveAll(root); err != nil {
		log.Printf("[CONTENTVER] failed to remove mod root %s: %v", root, err)
	}
	if err := os.RemoveAll(addonsRoot); err != nil {
		log.Printf("[CONTENTVER] failed to remove addons root %s: %v", addonsRoot, err)
	}
	return database.DeleteAllModGenerations(modID)
}
