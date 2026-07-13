package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
)

// BranchInstallSnapshot is the install progress for a branch.
type BranchInstallSnapshot struct {
	BranchID   int     `json:"branchId"`
	Installing bool    `json:"installing"`
	Message    string  `json:"message"`
	Percentage float64 `json:"percentage"`
	Error      string  `json:"error,omitempty"`
}

// BranchInstaller manages SteamCMD branch installs with SSE broadcasting.
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

// Install starts a SteamCMD install for the given branch.
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
	if err != nil || config == nil || config.Username == "" {
		b.setError("Steam credentials not configured")
		return
	}

	if err := os.MkdirAll(branch.InstallPath, 0755); err != nil {
		b.setError(fmt.Sprintf("failed to create install dir: %v", err))
		return
	}
	installDir, err := filepath.Abs(branch.InstallPath)
	if err != nil {
		installDir = branch.InstallPath
	}
	log.Printf("[BRANCH] installing branch %q (steam beta %q) to %s", branch.Name, branch.SteamBranch, installDir)

	progressChan := make(chan depot.ProgressUpdate, 100)
	installDone := make(chan error, 1)
	go func() {
		installDone <- depot.AppUpdate(depot.AppUpdateOptions{
			AppID:        depot.DedicatedServerAppID,
			SteamBranch:  branch.SteamBranch,
			InstallDir:   installDir,
			Username:     config.Username,
			Password:     config.Password,
			SteamCmdPath: config.SteamCmdPath,
		}, progressChan)
		close(progressChan)
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
	installed := IsBranchInstalled(branch.InstallPath)

	if installed {
		gameDir := BranchGameDir(branch.InstallPath)
		log.Printf("[BRANCH] install complete — game files at %s", gameDir)
		if err := database.SetBranchInstalled(branchID, true); err != nil {
			log.Printf("[BRANCH] failed to update branch status: %v", err)
		}
		b.mu.Lock()
		b.message = "Install complete"
		b.percentage = 100
		b.errMsg = ""
		b.mu.Unlock()
		b.broadcast()
		return
	}

	if installErr != nil {
		b.setError(installErr.Error())
		return
	}
	abs, _ := filepath.Abs(branch.InstallPath)
	b.setError(fmt.Sprintf("install finished but server binary not found in %s", abs))
	if alt := depot.FindSteamCmdDedicatedServerDir(config.SteamCmdPath); alt != "" {
		log.Printf("[BRANCH] server files may have been downloaded to SteamCMD library: %s", alt)
	}
}

func (b *BranchInstaller) setError(msg string) {
	b.mu.Lock()
	b.errMsg = msg
	b.message = msg
	b.mu.Unlock()
	b.broadcast()
}

// IsBranchInstalled checks if the branch has an arma3server binary.
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
