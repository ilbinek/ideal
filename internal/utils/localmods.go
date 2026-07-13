package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const localModsStampFile = ".sync-stamp"

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
	// Case-insensitive mod.cpp
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
		return nil, nil
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
		return nil, fmt.Errorf("local mods root is not a directory: %s", abs)
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
func SyncModMirror(src, destDir string) error {
	if src == "" {
		return fmt.Errorf("mod path is empty")
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

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
		targetPath := filepath.Join(destDir, strings.ToLower(relPath))
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
	return nil
}

func localModsStampPath(slug string) string {
	return filepath.Join(LocalModsDir(slug), localModsStampFile)
}

// BuildLocalModsStamp returns a deterministic stamp for the discovered folders.
func BuildLocalModsStamp(folders []LocalModFolder) string {
	lines := make([]string, 0, len(folders))
	for _, f := range folders {
		info, err := os.Stat(f.Path)
		mtime := int64(0)
		if err == nil {
			mtime = info.ModTime().Unix()
		}
		lines = append(lines, fmt.Sprintf("%s|%d", strings.ToLower(f.Name), mtime))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// LocalModsChanged reports whether the source root differs from the last sync stamp.
func LocalModsChanged(slug, root string) bool {
	folders, err := ListLocalModFolders(root)
	if err != nil {
		return true
	}
	want := BuildLocalModsStamp(folders)
	data, err := os.ReadFile(localModsStampPath(slug))
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) != want
}

// SyncServerLocalMods mirrors discovered mods into servers/<slug>/localmods and
// removes stale mirror directories. Writes a sync stamp. Does not touch the DB.
func SyncServerLocalMods(slug, root string) ([]LocalModFolder, error) {
	folders, err := ListLocalModFolders(root)
	if err != nil {
		return nil, err
	}
	mirrorRoot := LocalModsDir(slug)
	if err := os.MkdirAll(mirrorRoot, 0755); err != nil {
		return nil, err
	}

	wanted := make(map[string]struct{}, len(folders))
	for _, f := range folders {
		key := strings.ToLower(f.Name)
		wanted[key] = struct{}{}
		dest := filepath.Join(mirrorRoot, key)
		log.Printf("[LOCALMODS] syncing %s -> %s", f.Path, dest)
		if err := SyncModMirror(f.Path, dest); err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(mirrorRoot)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, ok := wanted[name]; ok {
			continue
		}
		log.Printf("[LOCALMODS] removing stale mirror %s", name)
		_ = os.RemoveAll(filepath.Join(mirrorRoot, name))
	}

	stamp := BuildLocalModsStamp(folders)
	if err := os.WriteFile(localModsStampPath(slug), []byte(stamp+"\n"), 0644); err != nil {
		return nil, err
	}
	return folders, nil
}
