package depot

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"arma3modmanager/internal/utils"

	"github.com/tbdevent/gosteam/pkg/app"
	"github.com/tbdevent/gosteam/pkg/cdn"
	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/downloader"
	"github.com/tbdevent/gosteam/pkg/manifest"
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
	var grandTotal uint64
	var lastPct float64

	emitAgg := func() {
		var completed uint64
		var anyFailed bool
		allDone := len(depotState) > 0
		for _, d := range depotState {
			completed += d.completed
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
		msg := "Downloading..."
		switch {
		case grandTotal == 0:
			msg = "Preparing download..."
			pct = 5
		case allDone:
			pct = 100
			msg = "Finalizing..."
		default:
			pct = float64(completed) / float64(grandTotal) * 100
			if pct < lastPct {
				pct = lastPct
			}
			if pct > 99.9 && !allDone {
				pct = 99.9
			}
		}
		if pct > lastPct {
			lastPct = pct
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
				st.done = false
				st.failed = false
				// Keep pre-resolved st.total so the overall denominator stays fixed.
			case downloader.PhaseDownloading:
				st.completed = p.CompletedBytes
				if p.TotalBytes > 0 && st.total == 0 {
					st.total = p.TotalBytes
				}
			case downloader.PhaseCompleted:
				if st.total > 0 {
					st.completed = st.total
				} else if p.TotalBytes > 0 {
					st.completed = p.TotalBytes
					st.total = p.TotalBytes
				} else {
					st.completed = p.CompletedBytes
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

	depots, err := ad.Resolve(ctx)
	if err != nil {
		send(ProgressUpdate{Stage: "error", Message: err.Error(), Error: err})
		return fmt.Errorf("gosteam app resolve: %w", err)
	}
	if len(depots) == 0 {
		msg := "no depots resolved for app"
		send(ProgressUpdate{Stage: "error", Message: msg})
		return fmt.Errorf("%s", msg)
	}

	send(ProgressUpdate{Stage: "connecting", Message: "Measuring depot sizes...", Percentage: 5})
	sizes, err := measureDepotSizes(ctx, sess, store, depots)
	if err != nil {
		send(ProgressUpdate{Stage: "error", Message: err.Error(), Error: err})
		return fmt.Errorf("gosteam measure depots: %w", err)
	}

	progMu.Lock()
	for _, d := range depots {
		key := downloader.FormatDepotKey(d)
		sz := sizes[key]
		depotState[key] = &depotProg{total: sz}
		grandTotal += sz
	}
	progMu.Unlock()

	log.Printf("[GOSTEAM] app_update app=%d branch=%q dir=%s depots=%d bytes=%d",
		appID, opts.SteamBranch, installDir, len(depots), grandTotal)

	send(ProgressUpdate{Stage: "downloading", Message: "Downloading... 0.0%", Percentage: 0})

	results := ad.DownloadDepots(ctx, depots)

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

// measureDepotSizes downloads each depot's manifest and returns per-depot
// uncompressed file totals (keyed like downloader.FormatDepotKey). Manifests
// are also written to the gosteam cache so the subsequent sync can reuse them
// on disk even though the downloader still refreshes from the CDN.
func measureDepotSizes(
	ctx context.Context,
	sess *client.Session,
	store *state.Store,
	depots []app.Depot,
) (map[string]uint64, error) {
	c, err := cdn.New(ctx, sess.Steam(), sess.CellID())
	if err != nil {
		return nil, err
	}
	resolver := app.NewResolver(sess.Steam())
	out := make(map[string]uint64, len(depots))
	for _, depot := range depots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		depotKey, err := c.DepotKey(ctx, depot.AppID, depot.DepotID)
		if err != nil {
			return nil, fmt.Errorf("depot %d key: %w", depot.DepotID, err)
		}
		mopts := cdn.ManifestOpts{Branch: depot.Branch}
		if hash := resolver.BranchPasswordHash(depot.Branch); hash != "" {
			mopts.BranchPasswordHash = hash
		}
		m, raw, err := c.DownloadManifestOpts(ctx, depot.AppID, depot.DepotID, depot.ManifestID, depotKey, mopts)
		if err != nil {
			return nil, fmt.Errorf("depot %d manifest: %w", depot.DepotID, err)
		}
		if err := manifest.Save(store.ManifestCachePath(depot.DepotID, depot.ManifestID), raw); err != nil {
			return nil, fmt.Errorf("depot %d manifest cache: %w", depot.DepotID, err)
		}
		var total uint64
		for i := range m.Files {
			total += m.Files[i].Size
		}
		out[downloader.FormatDepotKey(depot)] = total
	}
	return out, nil
}
