package contentver

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HardlinkClone recreates the directory tree at dst, hardlinking files from src.
// Falls back to copying a file when hardlinks are unsupported.
func HardlinkClone(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if src == dst {
		return fmt.Errorf("hardlink clone: source and destination are the same")
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("hardlink clone: source is not a directory")
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	hardlinkOK := true
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			_ = os.Remove(target)
		}
		if hardlinkOK {
			if err := os.Link(path, target); err == nil {
				return nil
			} else {
				hardlinkOK = false
				log.Printf("[CONTENTVER] hardlink unsupported, falling back to copy: %v", err)
			}
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// IsGenerationName reports whether name is a positive integer generation folder.
func IsGenerationName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	n, err := strconv.Atoi(name)
	return err == nil && n > 0
}

// HasGenerationLayout reports whether dir contains only generation subdirs (or is empty).
func HasGenerationLayout(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	if len(entries) == 0 {
		return true
	}
	sawGen := false
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !e.IsDir() || !IsGenerationName(name) {
			return false
		}
		sawGen = true
	}
	return sawGen
}

// MoveFlatContentsIntoGen moves non-generation top-level entries into genDir.
func MoveFlatContentsIntoGen(root, genDir string) error {
	if err := os.MkdirAll(filepath.Dir(genDir), 0755); err != nil {
		return err
	}
	tmp := filepath.Join(root, ".migrate-tmp")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	moved := 0
	for _, e := range entries {
		name := e.Name()
		if name == ".migrate-tmp" || strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() && IsGenerationName(name) {
			continue
		}
		src := filepath.Join(root, name)
		dst := filepath.Join(tmp, name)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("migrate move %s: %w", name, err)
		}
		moved++
	}
	if moved == 0 {
		_ = os.RemoveAll(tmp)
		return nil
	}
	if _, err := os.Stat(genDir); err == nil {
		// Target exists — merge by moving children.
		childEntries, err := os.ReadDir(tmp)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(genDir, 0755); err != nil {
			return err
		}
		for _, e := range childEntries {
			if err := os.Rename(filepath.Join(tmp, e.Name()), filepath.Join(genDir, e.Name())); err != nil {
				return err
			}
		}
		_ = os.RemoveAll(tmp)
		return nil
	}
	return os.Rename(tmp, genDir)
}
