package downloader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tbdevent/gosteam/pkg/app"
	"github.com/tbdevent/gosteam/pkg/cdn"
	"github.com/tbdevent/gosteam/pkg/client"
	"github.com/tbdevent/gosteam/pkg/manifest"
	"github.com/tbdevent/gosteam/pkg/state"
	syncpkg "github.com/tbdevent/gosteam/pkg/sync"
)

// AppConfig controls an application (multi-depot) download.
type AppConfig struct {
	AppID             uint32
	Branch            string // empty => "public"
	BranchPassword    string
	OutDir            string
	OS                string
	Arch              string
	Language          string
	AllPlatforms      bool
	AllArchs          bool
	AllLanguages      bool
	LowViolence       bool
	DepotIDs          []uint32
	ManifestIDs       []uint64
	MaxDownloads      int
	MaxConcurrentDeps int // depots downloaded in parallel; default 2
	VerifyAll         bool
	Prune             bool

	OnProgress       func(AppProgress)
	ProgressInterval time.Duration
}

// AppProgress is a real-time update about one depot within an app download.
type AppProgress struct {
	Depot          app.Depot
	Phase          Phase
	CompletedBytes uint64
	TotalBytes     uint64
	Stats          syncpkg.Stats
	Err            error
}

// Percent returns completion as 0..100.
func (p AppProgress) Percent() float64 {
	if p.TotalBytes == 0 {
		if p.Phase == PhaseCompleted {
			return 100
		}
		return 0
	}
	return float64(p.CompletedBytes) / float64(p.TotalBytes) * 100
}

// AppResult is the per-depot outcome of an app download.
type AppResult struct {
	Depot app.Depot
	Stats syncpkg.Stats
	Err   error
}

// AppDownloader downloads Steam application depots into a single install directory.
type AppDownloader struct {
	cdn      *cdn.Client
	resolver *app.Resolver
	syncer   *syncpkg.Syncer
	store    *state.Store
	cfg      AppConfig
}

// NewApp builds an AppDownloader for a logged-in session.
func NewApp(ctx context.Context, sess *client.Session, store *state.Store, cfg AppConfig) (*AppDownloader, error) {
	if cfg.MaxConcurrentDeps <= 0 {
		cfg.MaxConcurrentDeps = 2
	}
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = 100 * time.Millisecond
	}
	if cfg.Branch == "" {
		cfg.Branch = app.DefaultBranch
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
	return &AppDownloader{
		cdn:      c,
		resolver: app.NewResolver(sess.Steam()),
		syncer:   syncer,
		store:    store,
		cfg:      cfg,
	}, nil
}

// Resolve expands the configured app into depots for the chosen branch.
func (d *AppDownloader) Resolve(ctx context.Context) ([]app.Depot, error) {
	return d.resolver.Resolve(ctx, app.ResolveOptions{
		AppID:          d.cfg.AppID,
		Branch:         d.cfg.Branch,
		BranchPassword: d.cfg.BranchPassword,
		OS:             d.cfg.OS,
		Arch:           d.cfg.Arch,
		Language:       d.cfg.Language,
		AllPlatforms:   d.cfg.AllPlatforms,
		AllArchs:       d.cfg.AllArchs,
		AllLanguages:   d.cfg.AllLanguages,
		LowViolence:    d.cfg.LowViolence,
		DepotIDs:       d.cfg.DepotIDs,
		ManifestIDs:    d.cfg.ManifestIDs,
	})
}

// BranchBuildID returns the Steam buildid for the configured branch from
// cached PICS appinfo (populated by Resolve).
func (d *AppDownloader) BranchBuildID() string {
	return d.resolver.BranchBuildID(d.cfg.AppID, d.cfg.Branch)
}

// Download resolves and downloads the app's depots concurrently into OutDir.
func (d *AppDownloader) Download(ctx context.Context) ([]AppResult, error) {
	depots, err := d.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	return d.DownloadDepots(ctx, depots), nil
}

// DownloadDepots downloads already-resolved depots concurrently.
func (d *AppDownloader) DownloadDepots(ctx context.Context, depots []app.Depot) []AppResult {
	results := make([]AppResult, len(depots))
	sem := make(chan struct{}, d.cfg.MaxConcurrentDeps)
	var wg sync.WaitGroup

	for i := range depots {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i] = AppResult{Depot: depots[i], Err: ctx.Err()}
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			stats, err := d.downloadDepot(ctx, depots[i])
			results[i] = AppResult{Depot: depots[i], Stats: stats, Err: err}
		}(i)
	}
	wg.Wait()
	return results
}

func (d *AppDownloader) emit(p AppProgress) {
	if d.cfg.OnProgress != nil {
		d.cfg.OnProgress(p)
	}
}

func (d *AppDownloader) downloadDepot(ctx context.Context, depot app.Depot) (stats syncpkg.Stats, err error) {
	d.emit(AppProgress{Depot: depot, Phase: PhaseStarted})
	defer func() {
		if err != nil {
			d.emit(AppProgress{Depot: depot, Phase: PhaseFailed, Err: err})
		}
	}()

	depotKey, err := d.cdn.DepotKey(ctx, depot.AppID, depot.DepotID)
	if err != nil {
		return stats, err
	}

	opts := cdn.ManifestOpts{Branch: depot.Branch}
	if hash := d.resolver.BranchPasswordHash(depot.Branch); hash != "" {
		opts.BranchPasswordHash = hash
	}

	newM, raw, err := d.cdn.DownloadManifestOpts(ctx, depot.AppID, depot.DepotID, depot.ManifestID, depotKey, opts)
	if err != nil {
		return stats, err
	}

	var total uint64
	for i := range newM.Files {
		total += newM.Files[i].Size
	}

	var oldM *manifest.Manifest
	if oldID := d.store.InstalledAppManifest(depot.AppID, depot.DepotID); oldID != 0 {
		oldM, _ = manifest.Load(d.store.ManifestCachePath(depot.DepotID, oldID), depotKey)
	}

	if err = manifest.Save(d.store.ManifestCachePath(depot.DepotID, depot.ManifestID), raw); err != nil {
		return stats, err
	}

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
		d.emit(AppProgress{Depot: depot, Phase: PhaseDownloading, CompletedBytes: completed, TotalBytes: tot})
	}

	stats, err = d.syncer.Sync(ctx, syncpkg.Target{
		AppID:      depot.AppID,
		DepotID:    depot.DepotID,
		DepotKey:   depotKey,
		New:        newM,
		Old:        oldM,
		OutDir:     d.cfg.OutDir,
		OnProgress: onProgress,
	})
	if err != nil {
		return stats, err
	}

	if err = d.store.SetInstalledAppManifest(depot.AppID, depot.DepotID, depot.ManifestID); err != nil {
		return stats, err
	}

	d.emit(AppProgress{Depot: depot, Phase: PhaseCompleted, CompletedBytes: total, TotalBytes: total, Stats: stats})
	return stats, nil
}

// FormatDepotKey is a stable UI key for a depot.
func FormatDepotKey(d app.Depot) string {
	return fmt.Sprintf("%d:%d", d.AppID, d.DepotID)
}
