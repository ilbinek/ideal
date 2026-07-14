package depot

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"arma3modmanager/internal/utils"

	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/downloader"
	"github.com/tbdevent/gosteam/pkg/state"
)

// DedicatedServerAppIDNum is the Arma 3 dedicated server Steam AppID.
const DedicatedServerAppIDNum uint32 = 233780

// AppUpdateGoSteamOptions configures a built-in gosteam app install.
type AppUpdateGoSteamOptions struct {
	AppID       uint32 // empty/0 => DedicatedServerAppIDNum
	SteamBranch string // empty = public
	InstallDir  string
}

// AppUpdateGoSteam installs or updates a Steam app via the built-in gosteam
// library (delta-patched into InstallDir), streaming progress suitable for the
// branch-install SSE UI.
func AppUpdateGoSteam(
	ctx context.Context,
	sess *client.Session,
	store *state.Store,
	opts AppUpdateGoSteamOptions,
	progressChan chan<- ProgressUpdate,
) error {
	appID := opts.AppID
	if appID == 0 {
		appID = DedicatedServerAppIDNum
	}
	installDir := opts.InstallDir
	if abs, err := filepath.Abs(installDir); err == nil {
		installDir = abs
	}

	send := func(u ProgressUpdate) {
		if progressChan != nil {
			progressChan <- u
		}
	}

	send(ProgressUpdate{Stage: "connecting", Message: "Resolving depots...", Percentage: 2})

	var progMu sync.Mutex
	type depotProg struct {
		completed, total uint64
		done             bool
		failed           bool
	}
	depotState := make(map[string]*depotProg)

	emitAgg := func() {
		var completed, total uint64
		var anyFailed bool
		allDone := len(depotState) > 0
		for _, d := range depotState {
			completed += d.completed
			total += d.total
			if d.failed {
				anyFailed = true
			}
			if !d.done && !d.failed {
				allDone = false
			}
		}
		if anyFailed {
			return
		}
		pct := float64(0)
		if total > 0 {
			pct = float64(completed) / float64(total) * 100
		}
		msg := "Downloading..."
		if allDone && total > 0 {
			pct = 100
			msg = "Finalizing..."
		} else if total == 0 {
			msg = "Preparing download..."
			pct = 5
		}
		send(ProgressUpdate{Stage: "downloading", Message: fmt.Sprintf("%s %.1f%%", msg, pct), Percentage: pct})
	}

	ad, err := downloader.NewApp(ctx, sess, store, downloader.AppConfig{
		AppID:  appID,
		Branch: opts.SteamBranch,
		OutDir: installDir,
		Prune:  true,
		OnProgress: func(p downloader.AppProgress) {
			key := downloader.FormatDepotKey(p.Depot)
			progMu.Lock()
			defer progMu.Unlock()
			st, ok := depotState[key]
			if !ok {
				st = &depotProg{}
				depotState[key] = st
			}
			switch p.Phase {
			case downloader.PhaseStarted:
				st.completed = 0
				st.total = 0
				st.done = false
				st.failed = false
			case downloader.PhaseDownloading:
				st.completed = p.CompletedBytes
				st.total = p.TotalBytes
			case downloader.PhaseCompleted:
				st.completed = p.CompletedBytes
				if p.TotalBytes > 0 {
					st.total = p.TotalBytes
				} else {
					st.total = p.CompletedBytes
				}
				st.done = true
			case downloader.PhaseFailed:
				st.failed = true
				msg := "Depot download failed"
				if p.Err != nil {
					msg = p.Err.Error()
				}
				send(ProgressUpdate{Stage: "error", Message: msg, Error: p.Err})
				return
			}
			emitAgg()
		},
	})
	if err != nil {
		send(ProgressUpdate{Stage: "error", Message: err.Error(), Error: err})
		return fmt.Errorf("init gosteam app downloader: %w", err)
	}

	log.Printf("[GOSTEAM] app_update app=%d branch=%q dir=%s", appID, opts.SteamBranch, installDir)

	results, err := ad.Download(ctx)
	if err != nil {
		send(ProgressUpdate{Stage: "error", Message: err.Error(), Error: err})
		return fmt.Errorf("gosteam app resolve: %w", err)
	}

	var firstErr error
	for _, r := range results {
		if r.Err != nil {
			firstErr = r.Err
			log.Printf("[GOSTEAM] depot %s failed: %v", downloader.FormatDepotKey(r.Depot), r.Err)
		}
	}
	if firstErr != nil {
		msg := firstErr.Error()
		send(ProgressUpdate{Stage: "error", Message: msg, Error: firstErr})
		return fmt.Errorf("gosteam app update failed: %w", firstErr)
	}

	if !dedicatedServerInstalled(installDir) {
		abs, _ := filepath.Abs(installDir)
		gameDir := utils.DedicatedServerGameDir(installDir)
		msg := fmt.Sprintf("install finished but no server binary found in %s", abs)
		if gameDir != abs {
			msg += fmt.Sprintf(" (game dir checked: %s)", gameDir)
		}
		send(ProgressUpdate{Stage: "error", Message: msg})
		return fmt.Errorf("%s", msg)
	}

	send(ProgressUpdate{Stage: "complete", Message: "Install complete", Percentage: 100})
	return nil
}
