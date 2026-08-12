package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
	"arma3modmanager/internal/steam"
	"arma3modmanager/internal/steamcmdinfo"
	"arma3modmanager/internal/utils"
)

// BranchInstallSnapshot is the install progress for a branch.
type BranchInstallSnapshot struct {
	BranchID   int     `json:"branchId"`
	Installing bool    `json:"installing"`
	Message    string  `json:"message"`
	Percentage float64 `json:"percentage"`
	Error      string  `json:"error,omitempty"`
}

// BranchInstaller manages dedicated-server branch installs with SSE broadcasting.
type BranchInstaller struct {
	mu         sync.Mutex
	installing bool
	branchID   int
	message    string
	percentage float64
	errMsg     string

	subMu sync.Mutex
	subs  map[chan BranchInstallSnapshot]struct{}
}

// BranchInstallerDefault is the process-wide branch installer.
var BranchInstallerDefault = &BranchInstaller{subs: make(map[chan BranchInstallSnapshot]struct{})}

// Snapshot returns current install state.
func (b *BranchInstaller) Snapshot() BranchInstallSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BranchInstallSnapshot{
		BranchID:   b.branchID,
		Installing: b.installing,
		Message:    b.message,
		Percentage: b.percentage,
		Error:      b.errMsg,
	}
}

// Subscribe registers an SSE subscriber.
func (b *BranchInstaller) Subscribe() chan BranchInstallSnapshot {
	ch := make(chan BranchInstallSnapshot, 4)
	b.subMu.Lock()
	b.subs[ch] = struct{}{}
	b.subMu.Unlock()
	ch <- b.Snapshot()
	return ch
}

// Unsubscribe removes a subscriber.
func (b *BranchInstaller) Unsubscribe(ch chan BranchInstallSnapshot) {
	b.subMu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.subMu.Unlock()
}

func (b *BranchInstaller) broadcast() {
	snap := b.Snapshot()
	b.subMu.Lock()
	defer b.subMu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}

// Install starts a dedicated-server install for the given branch.
func (b *BranchInstaller) Install(branchID int) error {
	b.mu.Lock()
	if b.installing {
		b.mu.Unlock()
		return fmt.Errorf("another branch install is already in progress")
	}
	b.installing = true
	b.branchID = branchID
	b.message = "Starting..."
	b.percentage = 0
	b.errMsg = ""
	b.mu.Unlock()
	b.broadcast()

	go b.runInstall(branchID)
	return nil
}

func (b *BranchInstaller) runInstall(branchID int) {
	defer func() {
		b.mu.Lock()
		b.installing = false
		b.mu.Unlock()
		b.broadcast()
	}()

	branch, err := database.GetBranch(branchID)
	if err != nil {
		b.setError("branch not found")
		return
	}

	config, err := database.GetConfig()
	if err != nil || config == nil {
		b.setError("Settings not configured")
		return
	}

	useGoSteam := config.Downloader == "" || config.Downloader == depot.DownloaderGoSteam

	if !useGoSteam && config.Username == "" {
		b.setError("Steam credentials not configured")
		return
	}

	installDir, generation, err := contentver.PrepareBranchInstall(branchID, branch.InstallPath)
	if err != nil {
		b.setError(fmt.Sprintf("failed to prepare branch version: %v", err))
		return
	}
	installDirAbs, err := filepath.Abs(installDir)
	if err != nil {
		installDirAbs = installDir
	}
	backend := config.Downloader
	if backend == "" {
		backend = depot.DownloaderGoSteam
	}
	log.Printf("[BRANCH] installing branch %q (steam beta %q) via %s to %s (gen %d)",
		branch.Name, branch.SteamBranch, backend, installDirAbs, generation)

	// Update of an existing install: force full CDN manifest re-check (gosteam VerifyAll;
	// SteamCMD already always uses "validate").
	cur, _ := database.GetCurrentBranchGeneration(branchID)
	verifyAll := branch.Installed || (cur != nil && IsBranchGenInstalled(branch.InstallPath, cur.Generation))
	if verifyAll {
		b.mu.Lock()
		b.message = "Verifying and updating..."
		b.mu.Unlock()
		b.broadcast()
		log.Printf("[BRANCH] verify-all enabled for existing install at %s", installDirAbs)
	}

	progressChan := make(chan depot.ProgressUpdate, 100)
	installDone := make(chan error, 1)
	go func() {
		defer close(progressChan)
		if useGoSteam {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			username := config.Username
			if username == "" {
				username = steam.Default.Status().Username
			}
			sess, err := steam.Default.EnsureSession(ctx, username)
			if err != nil {
				installDone <- err
				return
			}
			installDone <- depot.AppUpdateGoSteam(ctx, sess, steam.Default.Store(), depot.AppUpdateGoSteamOptions{
				AppID:       depot.DedicatedServerAppIDNum,
				SteamBranch: branch.SteamBranch,
				InstallDir:  installDirAbs,
				VerifyAll:   verifyAll,
			}, progressChan)
			return
		}
		installDone <- depot.AppUpdate(depot.AppUpdateOptions{
			AppID:        depot.DedicatedServerAppID,
			SteamBranch:  branch.SteamBranch,
			InstallDir:   installDirAbs,
			Username:     config.Username,
			Password:     config.Password,
			SteamCmdPath: config.SteamCmdPath,
		}, progressChan)
	}()

	for update := range progressChan {
		b.mu.Lock()
		if update.Message != "" {
			b.message = update.Message
		}
		if update.Percentage > 0 {
			b.percentage = update.Percentage
		}
		if update.Stage == "error" {
			b.errMsg = update.Message
		}
		b.mu.Unlock()
		b.broadcast()
	}

	installErr := <-installDone
	if installErr != nil {
		contentver.AbortBranchInstall(branchID, branch.InstallPath, generation)
		// Prefer the real download/update failure even if an older server binary
		// is still on disk (e.g. CDN HTTP 503 mid-update).
		b.setError(installErr.Error())
		return
	}

	if !IsBranchInstalled(installDir) {
		contentver.AbortBranchInstall(branchID, branch.InstallPath, generation)
		abs, _ := filepath.Abs(installDir)
		b.setError(fmt.Sprintf("install finished but server binary not found in %s", abs))
		if !useGoSteam {
			if alt := depot.FindSteamCmdDedicatedServerDir(config.SteamCmdPath); alt != "" {
				log.Printf("[BRANCH] server files may have been downloaded to SteamCMD library: %s", alt)
			}
		}
		return
	}

	gameDir := BranchGameDir(installDir)
	log.Printf("[BRANCH] install complete — game files at %s", gameDir)
	utils.EnsureDedicatedServerBinariesExecutable(installDir)
	buildID := depot.ReadInstalledBuildID(installDir)
	if buildID == "" {
		// gosteam does not write appmanifest_*.acf; fall back to the remote build ID
		// so update checks can detect the next release.
		if remote, err := steamcmdinfo.GetDedicatedServerBranches(); err != nil {
			log.Printf("[BRANCH] could not resolve remote build ID: %v", err)
		} else {
			key := steamcmdinfo.NormalizeSteamBranch(branch.SteamBranch)
			if info, ok := remote[key]; ok && info.BuildID != "" {
				buildID = info.BuildID
				log.Printf("[BRANCH] using remote build ID %s for branch %q (%s)", buildID, branch.Name, key)
			}
		}
	}
	if err := contentver.FinalizeBranchInstall(branchID, branch.InstallPath, generation, buildID); err != nil {
		log.Printf("[BRANCH] failed to finalize branch status: %v", err)
		b.setError(fmt.Sprintf("install finished but failed to record version: %v", err))
		return
	}
	b.mu.Lock()
	b.message = "Install complete"
	b.percentage = 100
	b.errMsg = ""
	b.mu.Unlock()
	b.broadcast()
}

func (b *BranchInstaller) setError(msg string) {
	b.mu.Lock()
	b.errMsg = msg
	b.message = msg
	b.mu.Unlock()
	b.broadcast()
}

// IsBranchGenInstalled reports whether a specific branch generation has a server binary.
func IsBranchGenInstalled(installPath string, generation int) bool {
	return IsBranchInstalled(contentver.BranchGenerationDir(installPath, generation))
}

// BranchContentDirForServer returns the branch content directory to use for a server:
// pinned generation when running, otherwise current.
func BranchContentDirForServer(serverID int, branch *database.ArmaBranch) (string, error) {
	if pin, err := database.GetServerPin(serverID, database.ContentKindBranch, branch.ID); err == nil && pin != nil {
		dir := contentver.BranchGenerationDir(branch.InstallPath, pin.Generation)
		if IsBranchInstalled(dir) {
			return dir, nil
		}
	}
	return ResolveBranchContentDir(branch)
}

// ResolveBranchContentDir returns the on-disk content directory for a branch
// (current generation after migration).
func ResolveBranchContentDir(branch *database.ArmaBranch) (string, error) {
	dir, gen, err := contentver.CurrentBranchContentDir(branch.ID, branch.InstallPath)
	if err != nil {
		return "", err
	}
	if gen == 0 {
		// Not yet versioned / empty — use root (legacy flat or fresh).
		return contentver.BranchRoot(branch.InstallPath), nil
	}
	return dir, nil
}

// BranchIsInstalled reports whether the branch's current generation has a server binary.
func BranchIsInstalled(branch *database.ArmaBranch) bool {
	dir, err := ResolveBranchContentDir(branch)
	if err != nil {
		return false
	}
	if IsBranchInstalled(dir) {
		return true
	}
	// Pre-migration flat layout still under install root.
	return IsBranchInstalled(contentver.BranchRoot(branch.InstallPath))
}

// IsBranchInstalled checks if the path has an arma3server binary.
func IsBranchInstalled(installPath string) bool {
	_, ok := FindServerBinary(installPath)
	return ok
}

// ServerBinaryName returns the server executable name for the branch install.
func ServerBinaryName(installPath string) (string, error) {
	if name, ok := FindServerBinary(installPath); ok {
		return name, nil
	}
	abs, _ := filepath.Abs(installPath)
	return "", fmt.Errorf("no server binary found in %s", abs)
}

// instanceSkipEntries are never symlinked from branch into instance.
var instanceSkipEntries = map[string]bool{
	"profiles": true, "keys": true, "mpmissions": true,
	"mods": true, "servermods": true, "logs": true, "localmods": true,
	"server.cfg": true, "basic.cfg": true,
}
