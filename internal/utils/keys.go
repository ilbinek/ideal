package utils

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// FindBikeyFiles recursively finds all readable .bikey files in a directory.
// Broken symlinks (common in stale lowercase mirrors) are skipped.
func FindBikeyFiles(dir string) ([]string, error) {
	var bikeyFiles []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable entries instead of failing the whole scan.
			log.Printf("[KEYS] skip %s: %v", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".bikey") {
			return nil
		}
		// Lstat succeeds for dangling symlinks; Stat/Open would fail.
		if _, err := os.Stat(path); err != nil {
			log.Printf("[KEYS] skip unreadable key %s: %v", path, err)
			return nil
		}
		bikeyFiles = append(bikeyFiles, path)
		return nil
	})

	return bikeyFiles, err
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// CopyBikeysFromDir copies top-level .bikey files from srcDir into destDir.
func CopyBikeysFromDir(srcDir, destDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".bikey") {
			continue
		}
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(src); err != nil {
			log.Printf("[KEYS] skip unreadable key %s: %v", src, err)
			continue
		}
		if err := CopyFile(src, filepath.Join(destDir, name)); err != nil {
			log.Printf("[KEYS] failed to copy %s: %v", src, err)
			continue
		}
	}
	return nil
}

// ExtractKeys copies all .bikey files from mod directories to a keys directory.
// Individual missing/unreadable keys are skipped so a stale mirror cannot
// block server start.
func ExtractKeys(modPaths []string, keysDir string) error {
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}

	for _, modPath := range modPaths {
		bikeyFiles, err := FindBikeyFiles(modPath)
		if err != nil {
			log.Printf("[KEYS] scan failed for %s: %v", modPath, err)
			continue
		}

		for _, bikeyFile := range bikeyFiles {
			filename := filepath.Base(bikeyFile)
			destPath := filepath.Join(keysDir, filename)
			if err := CopyFile(bikeyFile, destPath); err != nil {
				log.Printf("[KEYS] failed to copy %s: %v", bikeyFile, err)
				continue
			}
		}
	}

	return nil
}
