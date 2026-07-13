package server

import (
	"fmt"
	"log"
	"strings"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

// SyncLocalModsRoot mirrors the server's local mods root into
// servers/<slug>/localmods and reconciles server_mods rows.
// When force is false, skips work if the sync stamp matches.
func SyncLocalModsRoot(serverID int, force bool) error {
	srv, err := database.GetServer(serverID)
	if err != nil {
		return err
	}
	root := strings.TrimSpace(srv.LocalModsRoot)
	if root == "" {
		return nil
	}
	if !force && !utils.LocalModsChanged(srv.Slug, root) {
		return nil
	}

	folders, err := utils.SyncServerLocalMods(srv.Slug, root)
	if err != nil {
		return fmt.Errorf("local mods sync failed: %w", err)
	}
	if err := database.ReconcileLocalRootMods(serverID, folders); err != nil {
		return fmt.Errorf("local mods reconcile failed: %w", err)
	}
	log.Printf("[LOCALMODS] server %d synced %d mod(s) from %s", serverID, len(folders), root)
	return nil
}
