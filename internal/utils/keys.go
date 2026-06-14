package utils

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FindBikeyFiles recursively finds all .bikey files in a directory
func FindBikeyFiles(dir string) ([]string, error) {
	var bikeyFiles []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".bikey") {
			bikeyFiles = append(bikeyFiles, path)
		}
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
		if err := CopyFile(filepath.Join(srcDir, name), filepath.Join(destDir, name)); err != nil {
			return err
		}
	}
	return nil
}

// ExtractKeys copies all .bikey files from mod directories to a keys directory
func ExtractKeys(modPaths []string, keysDir string) error {
	// Create keys directory
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}

	for _, modPath := range modPaths {
		bikeyFiles, err := FindBikeyFiles(modPath)
		if err != nil {
			return err
		}

		for _, bikeyFile := range bikeyFiles {
			filename := filepath.Base(bikeyFile)
			destPath := filepath.Join(keysDir, filename)

			if err := CopyFile(bikeyFile, destPath); err != nil {
				return err
			}
		}
	}

	return nil
}
