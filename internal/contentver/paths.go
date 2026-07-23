package contentver

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/utils"
)

const modsRelDir = "mods"

// ModLogicalPath returns the stored local_path for a workshop mod (no generation).
func ModLogicalPath(workshopID string) string {
	return filepath.ToSlash(filepath.Join(".", modsRelDir, workshopID))
}

// ModRoot returns the absolute directory that holds generation folders for a mod.
func ModRoot(workshopID string) string {
	return filepath.Join(utils.DataRoot(), modsRelDir, workshopID)
}

// ModGenerationDir returns the absolute path of one mod generation.
func ModGenerationDir(workshopID string, generation int) string {
	return filepath.Join(ModRoot(workshopID), strconv.Itoa(generation))
}

// AddonsGenerationDir returns the lowercase addons mirror for one mod generation.
func AddonsGenerationDir(workshopID string, generation int) string {
	return filepath.Join(utils.AddonsRoot(), strings.ToLower(workshopID), strconv.Itoa(generation))
}

// StagingRoot is a temporary parent used so downloaders write into OutputDir/<id>
// while the real content lives under mods/<id>/<gen>/.
func StagingRoot() string {
	return filepath.Join(utils.DataRoot(), modsRelDir, ".staging")
}

// LinkStagingTarget creates StagingRoot/<workshopID> → genDir (symlink).
func LinkStagingTarget(workshopID, genDir string) error {
	if err := os.MkdirAll(StagingRoot(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(genDir, 0755); err != nil {
		return err
	}
	link := filepath.Join(StagingRoot(), workshopID)
	absGen, err := filepath.Abs(genDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	}
	return os.Symlink(absGen, link)
}

// ClearStaging removes the staging symlink for a workshop item.
func ClearStaging(workshopID string) {
	link := filepath.Join(StagingRoot(), workshopID)
	_ = os.Remove(link)
}

// BranchRoot returns the absolute branch install root (holds generation folders).
func BranchRoot(installPath string) string {
	return utils.ResolveDataPath(installPath)
}

// BranchGenerationDir returns the absolute path of one branch generation.
func BranchGenerationDir(installPath string, generation int) string {
	return filepath.Join(BranchRoot(installPath), strconv.Itoa(generation))
}

// RelativeGenerationPath returns a stable relative path for DB storage.
func RelativeGenerationPath(kind string, id string, generation int) string {
	switch kind {
	case database.ContentKindMod:
		return filepath.ToSlash(filepath.Join(".", modsRelDir, id, strconv.Itoa(generation)))
	case database.ContentKindBranch:
		// id is unused; callers pass full relative install + gen via path helpers
		return ""
	default:
		return ""
	}
}

// BranchGenerationRelPath returns ./arma3/branches/<slug>/<gen> style path.
func BranchGenerationRelPath(installPath string, generation int) string {
	root := BranchRoot(installPath)
	rel, err := filepath.Rel(utils.DataRoot(), root)
	if err != nil {
		rel = root
	}
	return filepath.ToSlash(filepath.Join(".", rel, strconv.Itoa(generation)))
}

// EnsureModMigrated moves flat mod content into generation 1 and records it in the DB.
func EnsureModMigrated(modID int, workshopID string) error {
	root := ModRoot(workshopID)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("mod root is not a directory: %s", root)
	}

	if !HasGenerationLayout(root) {
		gen1 := ModGenerationDir(workshopID, 1)
		log.Printf("[CONTENTVER] migrating flat mod %s -> %s", root, gen1)
		if err := MoveFlatContentsIntoGen(root, gen1); err != nil {
			return err
		}
	}

	gens, err := listOnDiskGenerations(root)
	if err != nil {
		return err
	}
	if len(gens) == 0 {
		return nil
	}

	current, err := database.GetCurrentModGeneration(modID)
	if err != nil {
		return err
	}
	currentGen := 0
	if current != nil {
		currentGen = current.Generation
	} else {
		currentGen = gens[len(gens)-1]
	}

	for _, g := range gens {
		rel := RelativeGenerationPath(database.ContentKindMod, workshopID, g)
		isCurrent := g == currentGen
		if err := database.UpsertModGeneration(modID, g, rel, isCurrent); err != nil {
			return err
		}
		// Ensure addons mirror exists for each generation we keep.
		addons := AddonsGenerationDir(workshopID, g)
		if _, err := os.Stat(addons); os.IsNotExist(err) {
			if syncErr := utils.SyncModMirror(ModGenerationDir(workshopID, g), addons); syncErr != nil {
				log.Printf("[CONTENTVER] warning: addons mirror for mod %s gen %d: %v", workshopID, g, syncErr)
			}
		}
	}
	cleanupLegacyAddonsMirror(workshopID)
	return nil
}

// cleanupLegacyAddonsMirror removes pre-versioning flat files under addons/<id>/
// while keeping numeric generation directories.
func cleanupLegacyAddonsMirror(workshopID string) {
	root := filepath.Join(utils.AddonsRoot(), strings.ToLower(workshopID))
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() && IsGenerationName(name) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// EnsureBranchMigrated moves flat branch content into generation 1 and records it.
func EnsureBranchMigrated(branchID int, installPath string) error {
	root := BranchRoot(installPath)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("branch root is not a directory: %s", root)
	}

	if !HasGenerationLayout(root) {
		// Only migrate if there is actual content (not an empty placeholder dir).
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		hasContent := false
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				hasContent = true
				break
			}
		}
		if hasContent {
			gen1 := BranchGenerationDir(installPath, 1)
			log.Printf("[CONTENTVER] migrating flat branch %s -> %s", root, gen1)
			if err := MoveFlatContentsIntoGen(root, gen1); err != nil {
				return err
			}
		}
	}

	gens, err := listOnDiskGenerations(root)
	if err != nil {
		return err
	}
	if len(gens) == 0 {
		return nil
	}

	current, err := database.GetCurrentBranchGeneration(branchID)
	if err != nil {
		return err
	}
	currentGen := 0
	if current != nil {
		currentGen = current.Generation
	} else {
		currentGen = gens[len(gens)-1]
	}

	branch, _ := database.GetBranch(branchID)
	buildID := ""
	if branch != nil {
		buildID = branch.BuildID
	}

	for _, g := range gens {
		rel := BranchGenerationRelPath(installPath, g)
		isCurrent := g == currentGen
		bid := ""
		if isCurrent {
			bid = buildID
		}
		if err := database.UpsertBranchGeneration(branchID, g, rel, bid, isCurrent); err != nil {
			return err
		}
	}
	return nil
}

func listOnDiskGenerations(root string) ([]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var gens []int
	for _, e := range entries {
		if !e.IsDir() || !IsGenerationName(e.Name()) {
			continue
		}
		n, _ := strconv.Atoi(e.Name())
		gens = append(gens, n)
	}
	// sort ascending
	for i := 0; i < len(gens); i++ {
		for j := i + 1; j < len(gens); j++ {
			if gens[j] < gens[i] {
				gens[i], gens[j] = gens[j], gens[i]
			}
		}
	}
	return gens, nil
}

// CurrentModContentDir returns the absolute content directory for the current mod generation.
func CurrentModContentDir(modID int, workshopID string) (string, int, error) {
	if err := EnsureModMigrated(modID, workshopID); err != nil {
		return "", 0, err
	}
	cur, err := database.GetCurrentModGeneration(modID)
	if err != nil {
		return "", 0, err
	}
	if cur == nil {
		return "", 0, nil
	}
	return ModGenerationDir(workshopID, cur.Generation), cur.Generation, nil
}

// CurrentBranchContentDir returns the absolute content directory for the current branch generation.
func CurrentBranchContentDir(branchID int, installPath string) (string, int, error) {
	if err := EnsureBranchMigrated(branchID, installPath); err != nil {
		return "", 0, err
	}
	cur, err := database.GetCurrentBranchGeneration(branchID)
	if err != nil {
		return "", 0, err
	}
	if cur == nil {
		// Fallback: unmigrated / empty — use root (may still be flat for empty dirs).
		return BranchRoot(installPath), 0, nil
	}
	return BranchGenerationDir(installPath, cur.Generation), cur.Generation, nil
}

// ResolveModAddonsPath returns the addons mirror for a specific generation.
func ResolveModAddonsPath(workshopID string, generation int) string {
	return AddonsGenerationDir(workshopID, generation)
}
