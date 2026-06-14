package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

const (
	BranchesBaseDir = "./arma3/branches"
	ServersBaseDir  = "./servers"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// SlugFromName converts a display name into a filesystem-safe slug.
func SlugFromName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "server"
	}
	return s
}

// InstanceDir returns the on-disk path for a server instance.
func InstanceDir(slug string) string {
	return filepath.Join(utils.DataRoot(), "servers", slug)
}

// EnsureUniqueSlug appends a numeric suffix if the slug is taken.
func EnsureUniqueSlug(base string, exists func(slug string) bool) string {
	if !exists(base) {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
	return base + "-" + fmt.Sprintf("%d", os.Getpid())
}

// ValidateCustomModPath checks that a path looks like an Arma mod folder.
func ValidateCustomModPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	hasModCpp := fileExists(filepath.Join(path, "mod.cpp"))
	hasAddons := dirExists(filepath.Join(path, "addons"))
	if !hasModCpp && !hasAddons {
		return fmt.Errorf("path does not look like an Arma mod (missing mod.cpp or addons/)")
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FindServerBinary locates the dedicated server executable under installPath.
// Returns the path relative to installPath and whether it was found.
func FindServerBinary(installPath string) (string, bool) {
	return utils.FindDedicatedServerBinaryRel(installPath)
}

// BranchGameDir returns the directory containing server binaries and game data.
func BranchGameDir(installPath string) string {
	return utils.DedicatedServerGameDir(installPath)
}

// SanitizeModLabel creates a safe symlink folder name.
func SanitizeModLabel(label string) string {
	s := strings.TrimSpace(label)
	if s == "" {
		return "mod"
	}
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.ReplaceAll(s, "*", "-")
	s = strings.ReplaceAll(s, "?", "-")
	s = strings.ReplaceAll(s, "\"", "-")
	s = strings.ReplaceAll(s, "<", "-")
	s = strings.ReplaceAll(s, ">", "-")
	s = strings.ReplaceAll(s, "|", "-")
	return strings.ToLower(s)
}

// ModLinkName returns the symlink folder name and source path for a server mod entry.
// Managed mods symlink from the lowercased addons mirror, not the raw mods download folder.
func ModLinkName(sm database.ServerMod) (linkName, srcPath string, ok bool) {
	if sm.ModID != nil {
		mod, err := database.GetMod(*sm.ModID)
		if err != nil || !mod.Downloaded || mod.LocalPath == "" {
			return "", "", false
		}
		linkName = strings.ToLower(mod.WorkshopID)
		if linkName == "" {
			linkName = strings.ToLower(filepath.Base(mod.LocalPath))
		}
		srcPath = utils.AddonsModPath(mod.LocalPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			if syncErr := utils.SyncModToAddons(mod.LocalPath); syncErr != nil {
				return "", "", false
			}
		}
		return linkName, srcPath, true
	}
	if sm.CustomPath != "" {
		if err := ValidateCustomModPath(sm.CustomPath); err != nil {
			return "", "", false
		}
		return filepath.Base(sm.CustomPath), sm.CustomPath, true
	}
	return "", "", false
}

// DefaultServerCfg returns a minimal server.cfg template.
func DefaultServerCfg(hostname string) string {
	if hostname == "" {
		hostname = "Arma 3 Server"
	}
	return fmt.Sprintf(`hostname = "%s";
password = "";
passwordAdmin = "admin123";
serverCommandPassword = "";
maxPlayers = 32;
verifySignatures = 2;
allowedFilePatching = 0;
BattlEye = 1;
`, hostname)
}

// DefaultBasicCfg returns a minimal basic.cfg template.
func DefaultBasicCfg() string {
	return `language="English";
adapter=-1;
3D_Performance=1;
Resolution_W=0;
Resolution_H=0;
Resolution_Bpp=32;
`
}

// CreateSymlink creates a symlink at linkPath pointing to targetPath.
func CreateSymlink(targetPath, linkPath string) error {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	absLink, err := filepath.Abs(linkPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absLink); err == nil {
		if err := os.Remove(absLink); err != nil {
			log.Printf("[SERVER] warning: failed to remove existing link %s: %v", absLink, err)
		}
	}
	return os.Symlink(absTarget, absLink)
}

// RemoveDirContents removes all entries inside a directory without removing the dir itself.
func RemoveDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
