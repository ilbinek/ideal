package server

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arma3modmanager/internal/database"
)

// Steam beta name for the dedicated-server Creator DLC content branch.
const CreatorDlcSteamBranch = "creatordlc"

// Known Creator DLC folder codes and display names.
var knownCdlcNames = map[string]string{
	"gm":   "Global Mobilization",
	"vn":   "S.O.G. Prairie Fire",
	"ws":   "Western Sahara",
	"spe":  "Spearhead 1944",
	"csla": "CSLA Iron Curtain",
	"rf":   "Reaction Forces",
	"ef":   "Expeditionary Forces",
}

// Core game dirs / first-party DLCs that must never be treated as cDLC packs.
var cdlcCoreSkip = map[string]bool{
	"addons": true, "dta": true, "battleye": true, "keys": true,
	"steamapps": true, "launcher": true, "curator": true, "expansion": true,
	"heli": true, "jets": true, "kart": true, "mark": true, "orange": true,
	"argo": true, "tank": true, "enoch": true, // Contact is first-party DLC
	"tacops": true, "aow": true, // Tac-Ops / Art of War — first-party, not Creator DLC
	"profiles": true, "mpmissions": true, "missions": true, "logs": true,
	"mods": true, "servermods": true, "localmods": true,
}

// CdlcInfo describes one Creator DLC pack for the UI/API.
type CdlcInfo struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// CdlcCatalog is the GET /api/cdlcs response body.
type CdlcCatalog struct {
	Installed bool       `json:"installed"`
	Items     []CdlcInfo `json:"items"`
}

// CreatorDlcBranch returns the Creator DLC arma_branches row, if present.
func CreatorDlcBranch() (*database.ArmaBranch, error) {
	return database.GetBranchBySteamBranch(CreatorDlcSteamBranch)
}

// CreatorDlcGameDir returns the game content root for the creatordlc install, or "".
func CreatorDlcGameDir() string {
	b, err := CreatorDlcBranch()
	if err != nil || b == nil {
		return ""
	}
	if !BranchIsInstalled(b) {
		return ""
	}
	dir, err := ResolveBranchContentDir(b)
	if err != nil {
		return ""
	}
	return BranchGameDir(dir)
}

// ListAvailableCdlcs scans the Creator DLC install for known and discovered packs.
func ListAvailableCdlcs() CdlcCatalog {
	gameDir := CreatorDlcGameDir()
	catalog := CdlcCatalog{Installed: gameDir != "", Items: []CdlcInfo{}}

	present := make(map[string]bool)
	if gameDir != "" {
		entries, err := os.ReadDir(gameDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				code := strings.ToLower(e.Name())
				if cdlcCoreSkip[code] || instanceSkipEntries[e.Name()] {
					continue
				}
				_, known := knownCdlcNames[code]
				if !known {
					addonsPath := filepath.Join(gameDir, e.Name(), "addons")
					if st, err := os.Stat(addonsPath); err != nil || !st.IsDir() {
						continue
					}
				}
				present[code] = true
			}
		}
	}

	seen := make(map[string]bool)
	for code, name := range knownCdlcNames {
		catalog.Items = append(catalog.Items, CdlcInfo{
			Code:      code,
			Name:      name,
			Available: present[code],
		})
		seen[code] = true
	}
	for code := range present {
		if seen[code] {
			continue
		}
		catalog.Items = append(catalog.Items, CdlcInfo{
			Code:      code,
			Name:      code,
			Available: true,
		})
	}
	sort.Slice(catalog.Items, func(i, j int) bool {
		return catalog.Items[i].Code < catalog.Items[j].Code
	})
	return catalog
}

// ResolveCdlcPath finds a cDLC folder: prefer creatordlc install, then fallbackGameDir.
func ResolveCdlcPath(code, fallbackGameDir string) (string, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" || cdlcCoreSkip[code] {
		return "", false
	}
	if gameDir := CreatorDlcGameDir(); gameDir != "" {
		if p := cdlcDirIfPresent(gameDir, code); p != "" {
			return p, true
		}
	}
	if fallbackGameDir != "" {
		if p := cdlcDirIfPresent(fallbackGameDir, code); p != "" {
			return p, true
		}
	}
	return "", false
}

func cdlcDirIfPresent(gameDir, code string) string {
	entries, err := os.ReadDir(gameDir)
	if err != nil {
		p := filepath.Join(gameDir, code)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.EqualFold(e.Name(), code) {
			return filepath.Join(gameDir, e.Name())
		}
	}
	return ""
}

// FilterResolvableCdlcs keeps enabled codes that exist on disk (creatordlc or fallback).
func FilterResolvableCdlcs(codes []string, fallbackGameDir string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, code := range codes {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" || seen[code] {
			continue
		}
		if _, ok := ResolveCdlcPath(code, fallbackGameDir); !ok {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

// CreatorDlcBranchInstalled reports whether the creatordlc Steam branch is installed.
func CreatorDlcBranchInstalled() bool {
	b, err := CreatorDlcBranch()
	if err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		return false
	}
	return BranchIsInstalled(b)
}
