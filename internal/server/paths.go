package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

const (
	BranchesBaseDir = "./arma3/branches"
	ServersBaseDir  = "./servers"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// reservedSlugs are SPA top-level path segments and other HTTP prefixes that
// must not be used as server instance slugs.
var reservedSlugs = map[string]struct{}{
	"api":              {},
	"assets":           {},
	"branches":         {},
	"dl":               {},
	"login":            {},
	"mods":             {},
	"modsets":          {},
	"servers":          {},
	"settings":         {},
	"startup-command":  {},
	"users":            {},
}

// IsReservedSlug reports whether slug collides with a fixed HTTP path segment.
func IsReservedSlug(slug string) bool {
	_, ok := reservedSlugs[slug]
	return ok
}

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

// EnsureUniqueSlug appends a numeric suffix if the slug is taken or reserved.
func EnsureUniqueSlug(base string, exists func(slug string) bool) string {
	taken := func(s string) bool {
		return IsReservedSlug(s) || exists(s)
	}
	if !taken(base) {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken(candidate) {
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
	if !utils.LooksLikeArmaMod(path) {
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

// FindServerBinary locates the preferred dedicated server executable under installPath.
// Returns the path relative to installPath and whether it was found.
func FindServerBinary(installPath string) (string, bool) {
	return utils.FindDedicatedServerBinaryRel(installPath)
}

// ListServerBinaries returns basenames of all arma3server* executables in the branch.
func ListServerBinaries(installPath string) []string {
	rels := utils.ListDedicatedServerBinaryRels(installPath)
	out := make([]string, 0, len(rels))
	seen := make(map[string]struct{}, len(rels))
	for _, rel := range rels {
		name := filepath.Base(filepath.FromSlash(rel))
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// ResolveServerBinary picks the configured binary (basename) or auto-prefers 64-bit.
func ResolveServerBinary(installPath, preferred string) (string, bool) {
	return utils.ResolveDedicatedServerBinaryRel(installPath, preferred)
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
// Custom/local path mods symlink from servers/<slug>/localmods/<name>/.
func ModLinkName(slug string, sm database.ServerMod) (linkName, srcPath string, ok bool) {
	return ModLinkNameForServer(0, slug, sm)
}

// ModLinkNameForServer is like ModLinkName but respects content pins when serverID > 0.
func ModLinkNameForServer(serverID int, slug string, sm database.ServerMod) (linkName, srcPath string, ok bool) {
	if !sm.Enabled {
		return "", "", false
	}
	if sm.ModID != nil {
		mod, err := database.GetMod(*sm.ModID)
		if err != nil || !mod.Downloaded || mod.LocalPath == "" {
			return "", "", false
		}
		linkName = strings.ToLower(mod.WorkshopID)
		if linkName == "" {
			linkName = strings.ToLower(filepath.Base(mod.LocalPath))
		}
		srcPath = resolveModAddonsForServer(serverID, mod)
		if srcPath == "" {
			return "", "", false
		}
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			return "", "", false
		}
		return linkName, srcPath, true
	}
	if sm.CustomPath != "" {
		if err := ValidateCustomModPath(sm.CustomPath); err != nil {
			return "", "", false
		}
		// All custom/local paths get a lowercased file-symlink mirror (Linux casing).
		linkName = strings.ToLower(filepath.Base(sm.CustomPath))
		srcPath = utils.LocalModsMirrorPath(slug, sm.CustomPath)
		if syncErr := utils.SyncModMirror(sm.CustomPath, srcPath); syncErr != nil {
			return "", "", false
		}
		return linkName, srcPath, true
	}
	return "", "", false
}

// resolveModAddonsForServer returns the addons mirror path for a mod generation
// (pinned if the server has a pin, otherwise current).
func resolveModAddonsForServer(serverID int, mod *database.Mod) string {
	if err := contentver.EnsureModMigrated(mod.ID, mod.WorkshopID); err != nil {
		log.Printf("[SERVER] migrate mod %d: %v", mod.ID, err)
	}
	gen := 0
	if serverID > 0 {
		if pin, err := database.GetServerPin(serverID, database.ContentKindMod, mod.ID); err == nil && pin != nil {
			gen = pin.Generation
		}
	}
	if gen == 0 {
		cur, err := database.GetCurrentModGeneration(mod.ID)
		if err != nil || cur == nil {
			// Legacy flat addons path fallback.
			return utils.AddonsModPath(mod.LocalPath)
		}
		gen = cur.Generation
	}
	addons := contentver.AddonsGenerationDir(mod.WorkshopID, gen)
	if _, err := os.Stat(addons); os.IsNotExist(err) {
		content := contentver.ModGenerationDir(mod.WorkshopID, gen)
		if syncErr := utils.SyncModMirror(content, addons); syncErr != nil {
			log.Printf("[SERVER] sync addons for mod %s gen %d: %v", mod.WorkshopID, gen, syncErr)
			return ""
		}
	}
	return addons
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
headlessClients[] = {"127.0.0.1"};
localClient[] = {"127.0.0.1"};
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
