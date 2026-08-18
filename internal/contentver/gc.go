package contentver

import (
	"log"
	"os"

	"arma3modmanager/internal/database"
)

// GCModOrphans deletes non-current generations of a mod that have no pin refs.
func GCModOrphans(modID int, workshopID string) {
	gens, err := database.ListModGenerations(modID)
	if err != nil {
		log.Printf("[CONTENTVER] list mod gens for GC: %v", err)
		return
	}
	for _, g := range gens {
		if g.IsCurrent {
			continue
		}
		if modGenerationProtected(modID, g.Generation) {
			log.Printf("[CONTENTVER] skipping GC of in-progress mod %s generation %d", workshopID, g.Generation)
			continue
		}
		n, err := database.GenerationRefCount(database.ContentKindMod, modID, g.Generation)
		if err != nil || n > 0 {
			continue
		}
		log.Printf("[CONTENTVER] deleting unused mod %s generation %d", workshopID, g.Generation)
		_ = os.RemoveAll(ModGenerationDir(workshopID, g.Generation))
		_ = os.RemoveAll(AddonsGenerationDir(workshopID, g.Generation))
		_ = database.DeleteModGeneration(modID, g.Generation)
	}
}

// GCBranchOrphans deletes non-current branch generations with no pin refs.
func GCBranchOrphans(branchID int, installPath string) {
	gens, err := database.ListBranchGenerations(branchID)
	if err != nil {
		log.Printf("[CONTENTVER] list branch gens for GC: %v", err)
		return
	}
	for _, g := range gens {
		if g.IsCurrent {
			continue
		}
		if branchGenerationProtected(branchID, g.Generation) {
			log.Printf("[CONTENTVER] skipping GC of in-progress branch %d generation %d", branchID, g.Generation)
			continue
		}
		n, err := database.GenerationRefCount(database.ContentKindBranch, branchID, g.Generation)
		if err != nil || n > 0 {
			continue
		}
		log.Printf("[CONTENTVER] deleting unused branch %d generation %d", branchID, g.Generation)
		_ = os.RemoveAll(BranchGenerationDir(installPath, g.Generation))
		_ = database.DeleteBranchGeneration(branchID, g.Generation)
	}
}

// GCReleasedPins runs GC for content that was just unpinned.
func GCReleasedPins(pins []database.ServerContentPin) {
	seenMod := map[int]struct{}{}
	seenBranch := map[int]struct{}{}
	for _, p := range pins {
		switch p.Kind {
		case database.ContentKindMod:
			if _, ok := seenMod[p.RefID]; ok {
				continue
			}
			seenMod[p.RefID] = struct{}{}
			mod, err := database.GetMod(p.RefID)
			if err != nil || mod == nil {
				continue
			}
			GCModOrphans(mod.ID, mod.WorkshopID)
		case database.ContentKindBranch:
			if _, ok := seenBranch[p.RefID]; ok {
				continue
			}
			seenBranch[p.RefID] = struct{}{}
			branch, err := database.GetBranch(p.RefID)
			if err != nil || branch == nil {
				continue
			}
			GCBranchOrphans(branch.ID, branch.InstallPath)
		}
	}
}

// GCAllOrphans removes every non-current generation with zero refs (startup safety).
func GCAllOrphans() {
	mods, err := database.ListOrphanModGenerations()
	if err != nil {
		log.Printf("[CONTENTVER] list orphan mods: %v", err)
	} else {
		for _, g := range mods {
			if modGenerationProtected(g.ModID, g.Generation) {
				log.Printf("[CONTENTVER] skipping startup GC of in-progress mod %d generation %d", g.ModID, g.Generation)
				continue
			}
			mod, err := database.GetMod(g.ModID)
			if err != nil || mod == nil {
				_ = database.DeleteModGeneration(g.ModID, g.Generation)
				continue
			}
			log.Printf("[CONTENTVER] startup GC mod %s gen %d", mod.WorkshopID, g.Generation)
			_ = os.RemoveAll(ModGenerationDir(mod.WorkshopID, g.Generation))
			_ = os.RemoveAll(AddonsGenerationDir(mod.WorkshopID, g.Generation))
			_ = database.DeleteModGeneration(g.ModID, g.Generation)
		}
	}

	branches, err := database.ListOrphanBranchGenerations()
	if err != nil {
		log.Printf("[CONTENTVER] list orphan branches: %v", err)
		return
	}
	for _, g := range branches {
		if branchGenerationProtected(g.BranchID, g.Generation) {
			log.Printf("[CONTENTVER] skipping startup GC of in-progress branch %d generation %d", g.BranchID, g.Generation)
			continue
		}
		branch, err := database.GetBranch(g.BranchID)
		if err != nil || branch == nil {
			_ = database.DeleteBranchGeneration(g.BranchID, g.Generation)
			continue
		}
		log.Printf("[CONTENTVER] startup GC branch %d gen %d", g.BranchID, g.Generation)
		_ = os.RemoveAll(BranchGenerationDir(branch.InstallPath, g.Generation))
		_ = database.DeleteBranchGeneration(g.BranchID, g.Generation)
	}
}

// CollectPinsForServer builds pin rows for the server's current branch + workshop mods.
func CollectPinsForServer(serverID int) ([]database.ServerContentPin, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	var pins []database.ServerContentPin

	branch, err := database.GetBranch(srv.BranchID)
	if err != nil {
		return nil, err
	}
	if err := EnsureBranchMigrated(branch.ID, branch.InstallPath); err != nil {
		return nil, err
	}
	bcur, err := database.GetCurrentBranchGeneration(branch.ID)
	if err != nil {
		return nil, err
	}
	if bcur != nil {
		pins = append(pins, database.ServerContentPin{
			ServerID:   serverID,
			Kind:       database.ContentKindBranch,
			RefID:      branch.ID,
			Generation: bcur.Generation,
		})
	}

	serverMods, err := database.GetServerMods(serverID)
	if err != nil {
		return nil, err
	}
	for _, sm := range serverMods {
		if !sm.Enabled || sm.ModID == nil {
			continue
		}
		mod, err := database.GetMod(*sm.ModID)
		if err != nil || !mod.Downloaded {
			continue
		}
		if err := EnsureModMigrated(mod.ID, mod.WorkshopID); err != nil {
			log.Printf("[CONTENTVER] migrate mod %d for pin: %v", mod.ID, err)
			continue
		}
		mcur, err := database.GetCurrentModGeneration(mod.ID)
		if err != nil || mcur == nil {
			continue
		}
		pins = append(pins, database.ServerContentPin{
			ServerID:   serverID,
			Kind:       database.ContentKindMod,
			RefID:      mod.ID,
			Generation: mcur.Generation,
		})
	}
	return pins, nil
}
