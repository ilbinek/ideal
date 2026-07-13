package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const AddonsDirectory = "./addons"
const A3SyncDirectory = "./a3sync"

// AddonsRoot returns the addons directory under the IDEAL data root.
func AddonsRoot() string {
	return filepath.Join(DataRoot(), "addons")
}

// AddonsModPath returns the lowercased addons mirror path for a downloaded mod.
func AddonsModPath(modPath string) string {
	return filepath.Join(AddonsRoot(), strings.ToLower(filepath.Base(modPath)))
}

// SyncModToAddons creates a lowercased mirror of a mod in the addons directory with symlinks
func SyncModToAddons(modPath string) error {
	if modPath == "" {
		return fmt.Errorf("mod path is empty")
	}
	addonsModPath := AddonsModPath(modPath)
	log.Printf("[SYMLINK] Syncing mod from %s to %s", modPath, addonsModPath)
	if err := SyncModMirror(modPath, addonsModPath); err != nil {
		return fmt.Errorf("failed to sync mod to addons: %v", err)
	}
	log.Printf("[SYMLINK] Successfully synced mod to addons directory")
	return nil
}

// RemoveModFromAddons removes a mod's mirror from the addons directory
func RemoveModFromAddons(modPath string) error {
	if modPath == "" {
		return fmt.Errorf("mod path is empty")
	}

	addonsModPath := AddonsModPath(modPath)

	log.Printf("[SYMLINK] Removing mod from addons: %s", addonsModPath)

	// Check if the directory exists
	if _, err := os.Stat(addonsModPath); os.IsNotExist(err) {
		log.Printf("[SYMLINK] Addons directory does not exist: %s", addonsModPath)
		return nil // Not an error, just nothing to delete
	}

	// Remove the entire directory (this will remove all symlinks within it)
	if err := os.RemoveAll(addonsModPath); err != nil {
		return fmt.Errorf("failed to remove addons directory %s: %v", addonsModPath, err)
	}

	log.Printf("[SYMLINK] Successfully removed mod from addons directory")
	return nil
}

// EnsureAddonsDirectory creates the addons directory if it doesn't exist
func EnsureAddonsDirectory() error {
	if err := os.MkdirAll(AddonsRoot(), 0755); err != nil {
		return fmt.Errorf("failed to create addons directory: %v", err)
	}
	return nil
}

// SyncModToA3Sync recreates the mod structure with file symlinks in the a3sync directory
func SyncModToA3Sync(modPath, modName string) error {
	if modPath == "" {
		return fmt.Errorf("mod path is empty")
	}
	if modName == "" {
		return fmt.Errorf("mod name is empty")
	}

	// Sanitize the mod name for use as a folder name
	sanitizedName := sanitizeModName(modName)
	a3syncModPath := filepath.Join(A3SyncDirectory, sanitizedName)

	log.Printf("[A3SYNC] Syncing mod from %s to %s", modPath, a3syncModPath)

	// Walk through the mod directory and recreate structure with file symlinks
	err := filepath.Walk(modPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from modPath
		relPath, err := filepath.Rel(modPath, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Keep original casing (unlike addons which lowercases)
		targetPath := filepath.Join(a3syncModPath, relPath)

		if info.IsDir() {
			// Create directory
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %v", targetPath, err)
			}
		} else {
			// Create parent directory if needed
			targetDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("failed to create parent directory %s: %v", targetDir, err)
			}

			// Get absolute paths for symlink
			absModPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for %s: %v", path, err)
			}

			absTargetPath, err := filepath.Abs(targetPath)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for %s: %v", targetPath, err)
			}

			// Remove existing symlink if it exists
			if _, err := os.Lstat(absTargetPath); err == nil {
				if err := os.Remove(absTargetPath); err != nil {
					log.Printf("[A3SYNC] Warning: failed to remove existing symlink: %v", err)
				}
			}

			// Create symlink
			if err := os.Symlink(absModPath, absTargetPath); err != nil {
				return fmt.Errorf("failed to create symlink from %s to %s: %v", absTargetPath, absModPath, err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to sync mod to a3sync: %v", err)
	}

	log.Printf("[A3SYNC] Successfully synced mod to a3sync directory")
	return nil
}

// GenerateA3SyncFolder generates the entire a3sync folder from all downloaded mods
func GenerateA3SyncFolder(mods []struct{ LocalPath, Name string }) error {
	log.Printf("[A3SYNC] Generating a3sync folder from scratch")

	// Remove old a3sync directory if it exists
	if _, err := os.Stat(A3SyncDirectory); err == nil {
		log.Printf("[A3SYNC] Removing old a3sync directory")
		if err := os.RemoveAll(A3SyncDirectory); err != nil {
			return fmt.Errorf("failed to remove old a3sync directory: %v", err)
		}
	}

	// Create fresh a3sync directory
	if err := os.MkdirAll(A3SyncDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create a3sync directory: %v", err)
	}

	// Sync all downloaded mods
	for _, mod := range mods {
		if mod.LocalPath == "" {
			continue
		}

		log.Printf("[A3SYNC] Syncing mod: %s", mod.Name)
		if err := SyncModToA3Sync(mod.LocalPath, mod.Name); err != nil {
			log.Printf("[A3SYNC] Warning: failed to sync mod %s: %v", mod.Name, err)
			// Continue with other mods
		}
	}

	log.Printf("[A3SYNC] Successfully generated a3sync folder")
	return nil
}

// sanitizeModName removes characters that are problematic for folder names
func sanitizeModName(modName string) string {
	sanitized := strings.ReplaceAll(modName, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")
	sanitized = strings.ReplaceAll(sanitized, "*", "-")
	sanitized = strings.ReplaceAll(sanitized, "?", "-")
	sanitized = strings.ReplaceAll(sanitized, "\"", "-")
	sanitized = strings.ReplaceAll(sanitized, "<", "-")
	sanitized = strings.ReplaceAll(sanitized, ">", "-")
	sanitized = strings.ReplaceAll(sanitized, "|", "-")
	return strings.TrimSpace(sanitized)
}
