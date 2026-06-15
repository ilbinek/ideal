// Package downloader is the top-level orchestrator: it resolves workshop items,
// then downloads them concurrently over a single logged-in session, applying the
// delta engine per item and persisting installed-manifest state. This is the Go
// equivalent of DepotDownloader's DownloadAppAsync loop, specialized for bulk
// workshop downloads.
package downloader

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/tbdevent/gosteam/pkg/cdn"
	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/manifest"
	"github.com/tbdevent/gosteam/pkg/state"
	syncpkg "github.com/tbdevent/gosteam/pkg/sync"
	"github.com/tbdevent/gosteam/pkg/workshop"
)

// Config controls a bulk download.
type Config struct {
	AppID             uint32
	OutDir            string // content root; each item goes into OutDir/<pubFileID>
	MaxDownloads      int    // global cap on concurrent chunk downloads
	MaxConcurrentMods int    // how many items to process at once
	VerifyAll         bool
	Prune             bool

	// OnProgress, if set, receives lifecycle and progress events for every item.
	// It is called concurrently from multiple goroutines (one per in-flight mod),
	// so the implementation must be safe for concurrent use. This is the hook a
	// UI uses to show what is downloading and how far along it is.
	OnProgress func(Progress)

	// ProgressInterval throttles PhaseDownloading events per item (other phases
	// always fire). Defaults to 100ms.
	ProgressInterval time.Duration
}

// Phase identifies the stage of an item's lifecycle in a progress event.
type Phase int

const (
	// PhaseStarted: the item is resolved and downloading is about to begin.
	PhaseStarted Phase = iota
	// PhaseDownloading: a throttled progress update (CompletedBytes/TotalBytes).
	PhaseDownloading
	// PhaseCompleted: the item finished successfully (Stats is populated).
	PhaseCompleted
	// PhaseFailed: the item failed (Err is populated).
	PhaseFailed
)

func (p Phase) String() string {
	switch p {
	case PhaseStarted:
		return "started"
	case PhaseDownloading:
		return "downloading"
	case PhaseCompleted:
		return "completed"
	case PhaseFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Progress is a single real-time update about one item. Percent() is convenient
// for UIs.
type Progress struct {
	Item           workshop.Item
	Phase          Phase
	CompletedBytes uint64
	TotalBytes     uint64
	Stats          syncpkg.Stats // populated on PhaseCompleted
	Err            error         // populated on PhaseFailed
}

// Percent returns completion as 0..100 (0 when the total is unknown).
func (p Progress) Percent() float64 {
	if p.TotalBytes == 0 {
		if p.Phase == PhaseCompleted {
			return 100
		}
		return 0
	}
	return float64(p.CompletedBytes) / float64(p.TotalBytes) * 100
}

// Result is the per-item outcome of a bulk download.
type Result struct {
	Item  workshop.Item
	Stats syncpkg.Stats
	Err   error
}

// Downloader ties the session, CDN client, resolver, delta engine, and state
// store together.
type Downloader struct {
	cdn      *cdn.Client
	resolver *workshop.Resolver
	syncer   *syncpkg.Syncer
	store    *state.Store
	cfg      Config
}

// New builds a Downloader for a logged-in session.
func New(ctx context.Context, sess *client.Session, store *state.Store, cfg Config) (*Downloader, error) {
	if cfg.MaxConcurrentMods <= 0 {
		cfg.MaxConcurrentMods = 4
	}
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = 100 * time.Millisecond
	}
	c, err := cdn.New(ctx, sess.Steam(), sess.CellID())
	if err != nil {
		return nil, err
	}
	syncer := syncpkg.NewSyncer(c, syncpkg.Options{
		MaxDownloads: cfg.MaxDownloads,
		VerifyAll:    cfg.VerifyAll,
		Prune:        cfg.Prune,
	})
	return &Downloader{
		cdn:      c,
		resolver: workshop.NewResolver(sess.Steam()),
		syncer:   syncer,
		store:    store,
		cfg:      cfg,
	}, nil
}

// Resolve expands published-file ids (including collections) into items.
func (d *Downloader) Resolve(ctx context.Context, pubFileIDs []uint64) ([]workshop.Item, error) {
	return d.resolver.Resolve(ctx, d.cfg.AppID, pubFileIDs)
}

// Download resolves and downloads the given published-file ids concurrently. It
// returns a Result per item; an item-level failure does not abort the others.
// Real-time progress is delivered via Config.OnProgress.
func (d *Downloader) Download(ctx context.Context, pubFileIDs []uint64) ([]Result, error) {
	items, err := d.Resolve(ctx, pubFileIDs)
	if err != nil {
		return nil, err
	}
	return d.DownloadItems(ctx, items), nil
}

// DownloadItems downloads already-resolved items concurrently. Use this (after
// Resolve) when the caller wants the full item set up front, e.g. to size a
// progress UI. An item-level failure is reported in its Result and does not
// abort the others.
func (d *Downloader) DownloadItems(ctx context.Context, items []workshop.Item) []Result {
	results := make([]Result, len(items))
	sem := make(chan struct{}, d.cfg.MaxConcurrentMods)
	var wg sync.WaitGroup

	for i := range items {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i] = Result{Item: items[i], Err: ctx.Err()}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			stats, err := d.downloadItem(ctx, items[i])
			results[i] = Result{Item: items[i], Stats: stats, Err: err}
		}()
	}
	wg.Wait()
	return results
}

func (d *Downloader) emit(p Progress) {
	if d.cfg.OnProgress != nil {
		d.cfg.OnProgress(p)
	}
}

func (d *Downloader) downloadItem(ctx context.Context, item workshop.Item) (stats syncpkg.Stats, err error) {
	d.emit(Progress{Item: item, Phase: PhaseStarted})

	// Report a failure phase on any early return.
	defer func() {
		if err != nil {
			d.emit(Progress{Item: item, Phase: PhaseFailed, Err: err})
		}
	}()

	depotKey, err := d.cdn.DepotKey(ctx, item.AppID, item.DepotID)
	if err != nil {
		return stats, err
	}

	newM, raw, err := d.cdn.DownloadManifest(ctx, item.AppID, item.DepotID, item.ManifestID, depotKey)
	if err != nil {
		return stats, err
	}

	var total uint64
	for i := range newM.Files {
		total += newM.Files[i].Size
	}

	// Load the previously installed manifest for this item (if any) to diff against.
	var oldM *manifest.Manifest
	if oldID := d.store.InstalledManifest(item.PubFileID); oldID != 0 {
		oldM, _ = manifest.Load(d.store.ManifestCachePath(item.DepotID, oldID), depotKey)
	}

	// Cache the new manifest blob so it is available as the baseline next run.
	if err = manifest.Save(d.store.ManifestCachePath(item.DepotID, item.ManifestID), raw); err != nil {
		return stats, err
	}

	// Throttle PhaseDownloading events per item.
	var mu sync.Mutex
	var last time.Time
	onProgress := func(completed, tot uint64) {
		mu.Lock()
		now := time.Now()
		if completed < tot && now.Sub(last) < d.cfg.ProgressInterval {
			mu.Unlock()
			return
		}
		last = now
		mu.Unlock()
		d.emit(Progress{Item: item, Phase: PhaseDownloading, CompletedBytes: completed, TotalBytes: tot})
	}

	outDir := filepath.Join(d.cfg.OutDir, fmt.Sprint(item.PubFileID))
	stats, err = d.syncer.Sync(ctx, syncpkg.Target{
		AppID:      item.AppID,
		DepotID:    item.DepotID,
		DepotKey:   depotKey,
		New:        newM,
		Old:        oldM,
		OutDir:     outDir,
		OnProgress: onProgress,
	})
	if err != nil {
		return stats, err
	}

	if err = d.store.SetInstalledManifest(item.PubFileID, item.ManifestID); err != nil {
		return stats, err
	}

	d.emit(Progress{Item: item, Phase: PhaseCompleted, CompletedBytes: total, TotalBytes: total, Stats: stats})
	return stats, nil
}
