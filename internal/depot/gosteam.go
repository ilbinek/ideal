package depot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/downloader"
	"github.com/tbdevent/gosteam/pkg/state"
	"github.com/tbdevent/gosteam/pkg/workshop"
)

// GoSteamConfig tunes a built-in bulk download.
type GoSteamConfig struct {
	OutputDir         string // mods are placed at OutputDir/<WorkshopID>
	MaxConcurrentMods int    // how many mods download at once (default 4)
	MaxDownloads      int    // max concurrent chunk downloads across all mods (default 8)
}

// DownloadBatchGoSteam downloads many workshop items concurrently over a single
// logged-in session using the built-in gosteam library (delta-patched into
// OutputDir/<WorkshopID>). Per-item lifecycle is streamed via onProgress, keyed
// by the (string) workshop ID so callers can update their own per-mod state.
//
// The returned error is non-nil only for a batch-level failure (e.g. resolving
// items failed before any download started); per-item failures are reported
// through onProgress with Stage == "error".
func DownloadBatchGoSteam(
	ctx context.Context,
	sess *client.Session,
	store *state.Store,
	workshopIDs []string,
	cfg GoSteamConfig,
	onProgress func(workshopID string, u ProgressUpdate),
) error {
	ids := make([]uint64, 0, len(workshopIDs))
	for _, w := range workshopIDs {
		id, err := strconv.ParseUint(w, 10, 64)
		if err != nil {
			// Surface the bad id to its own row and skip it.
			if onProgress != nil {
				onProgress(w, ProgressUpdate{Stage: "error", Message: "invalid workshop id"})
			}
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	maxMods := cfg.MaxConcurrentMods
	if maxMods <= 0 {
		maxMods = 4
	}
	maxDownloads := cfg.MaxDownloads
	if maxDownloads <= 0 {
		maxDownloads = 8
	}

	d, err := downloader.New(ctx, sess, store, downloader.Config{
		AppID:             workshop.Arma3AppID,
		OutDir:            cfg.OutputDir,
		MaxConcurrentMods: maxMods,
		MaxDownloads:      maxDownloads,
		Prune:             true,
		OnProgress: func(p downloader.Progress) {
			if onProgress == nil {
				return
			}
			onProgress(strconv.FormatUint(p.Item.PubFileID, 10), goSteamProgress(p))
		},
	})
	if err != nil {
		return fmt.Errorf("init gosteam downloader: %w", err)
	}

	if _, err := d.Download(ctx, ids); err != nil {
		// Resolve-level failure: nothing downloaded. Item failures already
		// surfaced via onProgress, so only report a genuine batch error.
		return fmt.Errorf("gosteam download: %w", err)
	}
	return nil
}

// goSteamProgress maps a gosteam progress event onto the existing ProgressUpdate
// protocol so the queue/SSE pipeline is unchanged.
func goSteamProgress(p downloader.Progress) ProgressUpdate {
	switch p.Phase {
	case downloader.PhaseStarted:
		return ProgressUpdate{Stage: "downloading", Message: "Starting download...", Percentage: 0}
	case downloader.PhaseDownloading:
		pct := p.Percent()
		return ProgressUpdate{Stage: "downloading", Message: fmt.Sprintf("Downloading: %.1f%%", pct), Percentage: pct}
	case downloader.PhaseCompleted:
		return ProgressUpdate{Stage: "complete", Message: "Download complete", Percentage: 100}
	case downloader.PhaseFailed:
		msg := "Download failed"
		if p.Err != nil {
			msg = p.Err.Error()
		}
		return ProgressUpdate{Stage: "error", Message: msg, Error: p.Err}
	default:
		return ProgressUpdate{Stage: "downloading"}
	}
}
