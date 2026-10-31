package server

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

// duplicatePortStep is the offset between a server's ports and its copy's.
const duplicatePortStep = 100

// armaPortSpan is how many consecutive ports Arma uses from the game port.
const armaPortSpan = 5

// Instance entries that ProvisionServer regenerates; never copied.
var duplicateRegenerated = map[string]bool{
	"mods": true, "servermods": true, "keys": true, "localmods": true, "logs": true,
}

// Profile dirs that are caches, recreated by Arma.
var duplicateSkipProfileDirs = map[string]bool{
	"animdatacache": true, "datacache": true, "officialserverscache": true,
}

// Runtime output that should not be carried into the copy.
var duplicateSkipExts = map[string]bool{
	".rpt": true, ".log": true, ".mdmp": true, ".bidmp": true,
}

func serverPorts(gamePort, steamPort int) []int {
	ports := make([]int, 0, armaPortSpan+1)
	for p := gamePort; p < gamePort+armaPortSpan; p++ {
		ports = append(ports, p)
	}
	return append(ports, steamPort)
}

// PickDuplicatePorts returns ports for a copy of src that do not overlap any
// existing server: src ports shifted by the smallest multiple of 100 that is free.
func PickDuplicatePorts(src database.Server, all []database.Server) (gamePort, steamPort int, err error) {
	used := make(map[int]bool)
	for _, s := range all {
		for _, p := range serverPorts(s.GamePort, s.SteamPort) {
			used[p] = true
		}
	}
	for k := 1; ; k++ {
		off := k * duplicatePortStep
		g, st := src.GamePort+off, src.SteamPort+off
		if g+armaPortSpan > 65535 || st > 65535 {
			return 0, 0, fmt.Errorf("no free port range found above %d", src.GamePort)
		}
		free := true
		for _, p := range serverPorts(g, st) {
			if used[p] {
				free = false
				break
			}
		}
		if free {
			return g, st, nil
		}
	}
}

// duplicateSkip reports whether rel (slash-separated, relative to the instance
// root) should be left out of a server copy.
func duplicateSkip(rel string, d fs.DirEntry) bool {
	parts := strings.Split(strings.ToLower(rel), "/")
	if len(parts) == 1 {
		// Top-level symlinks are branch / cDLC links recreated by provisioning.
		if d.Type()&fs.ModeSymlink != 0 {
			return true
		}
		return duplicateRegenerated[parts[0]]
	}
	if parts[0] != "profiles" {
		return false
	}
	name := parts[len(parts)-1]
	if d.IsDir() {
		return duplicateSkipProfileDirs[name]
	}
	if duplicateSkipExts[filepath.Ext(name)] {
		return true
	}
	// BattlEye writes beserver*_active_*.cfg at runtime.
	return strings.HasPrefix(name, "beserver") && strings.Contains(name, "_active_")
}

// copyTree copies srcRoot into dstRoot. Regular files are copied, symlinks are
// recreated with the same target, and entries for which skip returns true are
// left out (whole subtree for directories).
func copyTree(srcRoot, dstRoot string, skip func(rel string, d fs.DirEntry) bool) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == srcRoot {
				return err
			}
			log.Printf("[DUPLICATE] skip %s: %v", path, err)
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstRoot, 0755)
		}
		if skip != nil && skip(filepath.ToSlash(rel), d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(dstRoot, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case d.IsDir():
			return os.MkdirAll(dst, 0755)
		case d.Type().IsRegular():
			return utils.CopyFile(path, dst)
		default:
			return nil // sockets, devices, …
		}
	})
}

var (
	serverCfgHostnameRe = regexp.MustCompile(`(?mi)^(\s*hostname\s*=\s*)"(?:[^"]|"")*"(\s*;)`)
	beRConPortRe        = regexp.MustCompile(`(?mi)^(\s*RConPort\s+)(\d+)`)
)

// setServerCfgHostname replaces the hostname string in server.cfg text.
func setServerCfgHostname(content, hostname string) string {
	quoted := `"` + strings.ReplaceAll(hostname, `"`, `""`) + `"`
	return serverCfgHostnameRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := serverCfgHostnameRe.FindStringSubmatch(m)
		return sub[1] + quoted + sub[2]
	})
}

// shiftRConPort moves a BattlEye RConPort by offset.
func shiftRConPort(content string, offset int) string {
	return beRConPortRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := beRConPortRe.FindStringSubmatch(m)
		port, err := strconv.Atoi(sub[2])
		if err != nil || port+offset > 65535 {
			return m
		}
		return sub[1] + strconv.Itoa(port+offset)
	})
}

func rewriteFile(path string, fn func(string) string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out := fn(string(data))
	if out == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0644)
}

// copyInstanceFiles copies user-owned instance files (configs, profiles,
// missions, custom keys, …) and adjusts them so the copy does not clash with
// the original.
func copyInstanceFiles(srcDir, dstDir, newName string, portOffset int) error {
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return os.MkdirAll(dstDir, 0755)
	}
	if err := copyTree(srcDir, dstDir, duplicateSkip); err != nil {
		return err
	}
	if err := rewriteFile(filepath.Join(dstDir, "server.cfg"), func(s string) string {
		return setServerCfgHostname(s, newName)
	}); err != nil {
		return fmt.Errorf("update server.cfg: %w", err)
	}
	beDirs, _ := filepath.Glob(filepath.Join(dstDir, "profiles", "*"))
	for _, dir := range beDirs {
		if !strings.EqualFold(filepath.Base(dir), "battleye") {
			continue
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if e.IsDir() || !strings.HasPrefix(name, "beserver") || !strings.HasSuffix(name, ".cfg") {
				continue
			}
			if err := rewriteFile(filepath.Join(dir, e.Name()), func(s string) string {
				return shiftRConPort(s, portOffset)
			}); err != nil {
				return fmt.Errorf("update %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// relocatePath maps a path inside oldRoot to the same place under newRoot;
// paths outside oldRoot are returned unchanged.
func relocatePath(path, oldRoot, newRoot string) string {
	rel, err := filepath.Rel(oldRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return path
	}
	return filepath.Join(newRoot, rel)
}

// DuplicateResult describes a newly created server copy.
type DuplicateResult struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	GamePort  int    `json:"gamePort"`
	SteamPort int    `json:"steamPort"`
}

// DuplicateServer creates an independent copy of a server: settings, mod list,
// custom keys, admins, headless setup and instance files (configs, profiles,
// missions). Generated content (mod links, keys, logs) is rebuilt by provisioning.
func DuplicateServer(srcID int, name string) (*DuplicateResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	src, err := database.GetServer(srcID)
	if err != nil {
		return nil, err
	}
	all, err := database.GetServers()
	if err != nil {
		return nil, err
	}
	gamePort, steamPort, err := PickDuplicatePorts(*src, all)
	if err != nil {
		return nil, err
	}
	slug := EnsureUniqueSlug(SlugFromName(name), func(s string) bool {
		for _, srv := range all {
			if srv.Slug == s {
				return true
			}
		}
		// A leftover folder must never be merged into.
		_, statErr := os.Stat(InstanceDir(s))
		return statErr == nil
	})

	srcDir, dstDir := InstanceDir(src.Slug), InstanceDir(slug)
	id, err := database.DuplicateServer(srcID, name, slug, gamePort, steamPort, func(p string) string {
		return relocatePath(p, srcDir, dstDir)
	})
	if err != nil {
		return nil, fmt.Errorf("copy server settings: %w", err)
	}
	cleanup := func() {
		if err := database.DeleteServer(int(id)); err != nil {
			log.Printf("[DUPLICATE] cleanup: delete server %d: %v", id, err)
		}
		if err := os.RemoveAll(dstDir); err != nil {
			log.Printf("[DUPLICATE] cleanup: remove %s: %v", dstDir, err)
		}
	}

	if err := copyInstanceFiles(srcDir, dstDir, name, gamePort-src.GamePort); err != nil {
		cleanup()
		return nil, fmt.Errorf("copy instance files: %w", err)
	}
	if src.HeadlessCount > 0 {
		if _, err := database.SyncHeadlessSlots(int(id), src.HeadlessCount, name, slug); err != nil {
			cleanup()
			return nil, fmt.Errorf("copy headless clients: %w", err)
		}
	}
	if err := ProvisionServer(int(id)); err != nil {
		// Same as create: the copy exists; provisioning is retried on start.
		log.Printf("[DUPLICATE] provision warning for %s: %v", slug, err)
	}
	log.Printf("[DUPLICATE] server %d (%s) duplicated as %d (%s) on :%d", srcID, src.Slug, id, slug, gamePort)
	return &DuplicateResult{ID: id, Slug: slug, Name: name, GamePort: gamePort, SteamPort: steamPort}, nil
}
