package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalModFolder is a discovered mod directory under a local mods root.
type LocalModFolder struct {
	Name string // original basename
	Path string // absolute source path
}

// LocalModsDir returns servers/<slug>/localmods under the data root.
func LocalModsDir(slug string) string {
	return filepath.Join(DataRoot(), "servers", slug, "localmods")
}

// LocalModsMirrorPath returns the lowercase mirror path for a mod basename.
func LocalModsMirrorPath(slug, modName string) string {
	return filepath.Join(LocalModsDir(slug), strings.ToLower(filepath.Base(modName)))
}

// HasAddonsDir reports whether path contains an addons directory (any case).
func HasAddonsDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), "addons") {
			return true
		}
	}
	return false
}

// LooksLikeArmaMod reports whether path looks like an Arma mod folder.
func LooksLikeArmaMod(path string) bool {
	if HasAddonsDir(path) {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "mod.cpp")); err == nil {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "mod.cpp") {
			return true
		}
	}
	return false
}

// ListLocalModFolders returns immediate child directories of root that look like mods.
func ListLocalModFolders(root string) ([]LocalModFolder, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", abs)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var out []LocalModFolder
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(abs, name)
		if !LooksLikeArmaMod(path) {
			continue
		}
		out = append(out, LocalModFolder{Name: name, Path: path})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// SyncModMirror creates a lowercased file-symlink mirror of src under destDir.
// Stale files left from previous syncs (e.g. renamed .bikey keys) are removed.
func SyncModMirror(src, destDir string) error {
	if src == "" {
		return fmt.Errorf("mod path is empty")
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	wanted := make(map[string]struct{})
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		relLower := strings.ToLower(relPath)
		wanted[filepath.Clean(relLower)] = struct{}{}
		targetPath := filepath.Join(destDir, relLower)
		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		absSrc, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		absTarget, err := filepath.Abs(targetPath)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(absTarget); err == nil {
			_ = os.Remove(absTarget)
		}
		return os.Symlink(absSrc, absTarget)
	})
	if err != nil {
		return fmt.Errorf("failed to sync mod mirror %s -> %s: %w", src, destDir, err)
	}

	// Drop mirror entries that no longer exist in the source (common after mod updates).
	_ = filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == destDir {
			return nil
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if _, ok := wanted[rel]; ok {
			return nil
		}
		if info.IsDir() {
			// Remove empty dirs later only if nothing wanted under them.
			hasWantedChild := false
			for w := range wanted {
				if strings.HasPrefix(w, rel+string(os.PathSeparator)) || strings.HasPrefix(w, rel+"/") {
					hasWantedChild = true
					break
				}
			}
			if !hasWantedChild {
				_ = os.RemoveAll(path)
				return filepath.SkipDir
			}
			return nil
		}
		log.Printf("[LOCALMODS] removing stale mirror file %s", path)
		_ = os.Remove(path)
		return nil
	})

	return nil
}
