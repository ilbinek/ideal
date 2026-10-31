package server

import (
	"log"
	"path/filepath"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

// modGenerationContentDir returns the raw content dir of a mod generation
// (pinned for serverID when set, otherwise current). Does not migrate legacy
// layouts; returns "" when no generation is recorded.
func modGenerationContentDir(serverID int, mod *database.Mod) string {
	gen := 0
	if serverID > 0 {
		if pin, err := database.GetServerPin(serverID, database.ContentKindMod, mod.ID); err == nil && pin != nil {
			gen = pin.Generation
		}
	}
	if gen == 0 {
		cur, err := database.GetCurrentModGeneration(mod.ID)
		if err != nil || cur == nil {
			return ""
		}
		gen = cur.Generation
	}
	return contentver.ModGenerationDir(mod.WorkshopID, gen)
}

// RefreshModSignature re-checks the current generation of a mod and stores the result.
func RefreshModSignature(modID int) {
	mod, err := database.GetMod(modID)
	if err != nil {
		return
	}
	var report *utils.SignatureReport
	if mod.Downloaded {
		if dir := modGenerationContentDir(0, mod); dir != "" {
			r := utils.CheckModSignatures(dir, nil)
			report = &r
		}
	}
	if err := database.SetModSignature(modID, report); err != nil {
		log.Printf("[SIGN] failed to store signature report for mod %d: %v", modID, err)
	}
}

// BackfillModSignatures checks downloaded mods that have no stored report yet.
func BackfillModSignatures() {
	mods, err := database.GetAllMods()
	if err != nil {
		log.Printf("[SIGN] backfill: %v", err)
		return
	}
	n := 0
	for _, m := range mods {
		if !m.Downloaded || m.Signature != nil {
			continue
		}
		RefreshModSignature(m.ID)
		n++
	}
	if n > 0 {
		log.Printf("[SIGN] checked signatures for %d mod(s)", n)
	}
}

// ServerModSignatures checks every server mod entry (keyed by server_mods.id)
// against its own bikeys plus every key already in the server's keys dir, so a
// key provided by a custom upload or another mod counts as present.
func ServerModSignatures(serverID int) (map[int]utils.SignatureReport, error) {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	mods, err := database.GetServerMods(serverID)
	if err != nil {
		return nil, err
	}
	serverKeys := utils.LoadKeySet(filepath.Join(InstanceDir(srv.Slug), "keys"))

	out := make(map[int]utils.SignatureReport, len(mods))
	for _, sm := range mods {
		dir := ""
		if sm.ModID != nil {
			if mod, err := database.GetMod(*sm.ModID); err == nil && mod.Downloaded {
				dir = modGenerationContentDir(serverID, mod)
			}
		} else if sm.CustomPath != "" {
			dir = sm.CustomPath
		}
		out[sm.ID] = utils.CheckModSignatures(dir, serverKeys)
	}
	return out, nil
}
