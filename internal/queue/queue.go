// Package queue provides a server-side sequential download queue for mods.
// It replaces the previous client-driven "click each button" approach: state
// lives on the server, so progress survives page reloads and the UI can simply
// subscribe to a single stream of snapshots.
package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
	"arma3modmanager/internal/server"
	"arma3modmanager/internal/steam"
)

// Item status values.
const (
	StatusQueued      = "queued"
	StatusDownloading = "downloading"
	StatusDone        = "done"
	StatusError       = "error"
)

// Item is a single mod in the current download batch.
type Item struct {
	ID         int     `json:"id"`
	WorkshopID string  `json:"workshopId"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Percentage float64 `json:"percentage"`
	Message    string  `json:"message"`
	FileName   string  `json:"fileName"`

	// generation is set while downloading into a soft-versioned folder.
	generation int
}

// Snapshot is the full, serializable state of the queue at a point in time.
type Snapshot struct {
	Processing bool   `json:"processing"`
	Active     int    `json:"active"` // mod ID currently downloading, 0 if none
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	Items      []Item `json:"items"`
}

// Manager owns the queue and broadcasts snapshots to subscribers.
type Manager struct {
	mu         sync.Mutex
	items      []*Item
	processing bool

	subMu sync.Mutex
	subs  map[chan Snapshot]struct{}
}

// Default is the process-wide queue.
var Default = &Manager{subs: make(map[chan Snapshot]struct{})}

// Enqueue adds the given mods to the queue (skipping ones already pending) and
// starts the worker if it isn't already running. Returns the number added.
func (m *Manager) Enqueue(mods []database.Mod) int {
	m.mu.Lock()
	// When idle, drop finished items from the previous run so each new batch
	// starts with a clean list.
	if !m.processing {
		kept := m.items[:0]
		for _, it := range m.items {
			if it.Status == StatusQueued || it.Status == StatusDownloading {
				kept = append(kept, it)
			}
		}
		m.items = kept
	}

	added := 0
	for _, mod := range mods {
		if m.hasPendingLocked(mod.ID) {
			continue
		}
		m.items = append(m.items, &Item{
			ID:         mod.ID,
			WorkshopID: mod.WorkshopID,
			Name:       mod.Name,
			Status:     StatusQueued,
		})
		added++
	}

	start := !m.processing && added > 0
	if start {
		m.processing = true
	}
	m.mu.Unlock()

	if added > 0 {
		m.broadcast()
	}
	if start {
		go m.worker()
	}
	return added
}

func (m *Manager) hasPendingLocked(id int) bool {
	for _, it := range m.items {
		if it.ID == id && (it.Status == StatusQueued || it.Status == StatusDownloading) {
			return true
		}
	}
	return false
}

// clearFinishedAfter waits delay, then removes done/error items if the queue is
// still idle. A new Enqueue while waiting cancels the clear (processing is true).
func (m *Manager) clearFinishedAfter(delay time.Duration) {
	time.Sleep(delay)
	m.mu.Lock()
	if m.processing {
		m.mu.Unlock()
		return
	}
	kept := m.items[:0]
	for _, it := range m.items {
		if it.Status == StatusQueued || it.Status == StatusDownloading {
			kept = append(kept, it)
		}
	}
	changed := len(kept) != len(m.items)
	m.items = kept
	m.mu.Unlock()
	if changed {
		m.broadcast()
	}
}

func (m *Manager) worker() {
	for {
		config, cfgErr := database.GetConfig()

		// Collect every currently-queued item. The gosteam path downloads the
		// whole batch concurrently; the legacy executables download one at a time.
		m.mu.Lock()
		var batch []*Item
		for _, it := range m.items {
			if it.Status == StatusQueued {
				batch = append(batch, it)
			}
		}
		if len(batch) == 0 {
			m.processing = false
			m.mu.Unlock()
			m.broadcast()
			// Drop finished/error items after a short pause so the Mods page can
			// show "Queue finished", then clear so the UI doesn't linger forever.
			go m.clearFinishedAfter(5 * time.Second)
			return
		}
		m.mu.Unlock()

		if cfgErr != nil || config == nil {
			for _, it := range batch {
				m.setError(it, "Steam credentials not configured")
			}
			m.broadcast()
			continue
		}

		if config.Downloader == depot.DownloaderGoSteam {
			m.mu.Lock()
			for _, it := range batch {
				it.Status = StatusDownloading
				it.Message = "Starting..."
				it.Percentage = 0
			}
			m.mu.Unlock()
			m.broadcast()

			m.downloadGoSteamBatch(config, batch)
			m.broadcast()
			continue
		}

		// Legacy sequential path: one external-process download at a time.
		current := batch[0]
		m.mu.Lock()
		current.Status = StatusDownloading
		current.Message = "Starting..."
		current.Percentage = 0
		m.mu.Unlock()
		m.broadcast()

		m.download(current)
		m.broadcast()
	}
}

// downloadGoSteamBatch downloads every item in batch concurrently over a single
// built-in Steam session, mapping per-mod progress back onto the queue items.
func (m *Manager) downloadGoSteamBatch(config *database.Config, batch []*Item) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := steam.Default.EnsureSession(ctx, config.Username)
	if err != nil {
		for _, it := range batch {
			m.setError(it, err.Error())
		}
		return
	}
	store := steam.Default.Store()

	byID := make(map[string]*Item, len(batch))
	ids := make([]string, 0, len(batch))
	for _, it := range batch {
		gen, err := contentver.PrepareModDownload(it.ID, it.WorkshopID)
		if err != nil {
			m.setError(it, "Failed to prepare version: "+err.Error())
			continue
		}
		it.generation = gen
		byID[it.WorkshopID] = it
		ids = append(ids, it.WorkshopID)
	}
	if len(ids) == 0 {
		return
	}

	// gosteam calls OnProgress concurrently (one goroutine per in-flight mod).
	onProgress := func(workshopID string, u depot.ProgressUpdate) {
		it, ok := byID[workshopID]
		if !ok {
			return
		}
		switch u.Stage {
		case "complete":
			m.finishGoSteamItem(it)
		case "error":
			contentver.AbortModDownload(it.ID, it.WorkshopID, it.generation)
			m.setError(it, u.Message)
			m.broadcast()
		default:
			m.mu.Lock()
			if u.Message != "" {
				it.Message = u.Message
			}
			if u.Percentage > 0 {
				it.Percentage = u.Percentage
			}
			if u.FileName != "" {
				it.FileName = u.FileName
			}
			m.mu.Unlock()
			m.broadcast()
		}
	}

	cfg := depot.GoSteamConfig{
		OutputDir:         contentver.StagingRoot(),
		MaxConcurrentMods: config.MaxConcurrentDownloads,
	}
	if err := depot.DownloadBatchGoSteam(ctx, sess, store, ids, cfg, onProgress); err != nil {
		log.Printf("[QUEUE] gosteam batch failed: %v", err)
		// Mark anything still in flight (never reported complete/error) as failed.
		for _, it := range batch {
			m.mu.Lock()
			pending := it.Status == StatusDownloading
			m.mu.Unlock()
			if pending {
				contentver.AbortModDownload(it.ID, it.WorkshopID, it.generation)
				m.setError(it, "Download failed: "+err.Error())
			}
		}
		m.broadcast()
	}
}

// finishGoSteamItem records a completed gosteam download in the DB and refreshes
// its addons mirror (rebuilt so symlinks for pruned files don't linger).
func (m *Manager) finishGoSteamItem(it *Item) {
	if err := contentver.FinalizeModDownload(it.ID, it.WorkshopID, it.generation); err != nil {
		log.Printf("[QUEUE] failed to finalize mod %s: %v", it.WorkshopID, err)
		contentver.AbortModDownload(it.ID, it.WorkshopID, it.generation)
		m.setError(it, "Failed to finalize download: "+err.Error())
		m.broadcast()
		return
	}
	server.RescanKeysForMod(it.ID)
	server.RefreshModSignature(it.ID)

	m.mu.Lock()
	it.Status = StatusDone
	it.Percentage = 100
	it.Message = "Download complete"
	m.mu.Unlock()
	m.broadcast()
}

func (m *Manager) download(item *Item) {
	config, err := database.GetConfig()
	if err != nil || config == nil {
		m.setError(item, "Steam credentials not configured")
		return
	}

	gen, err := contentver.PrepareModDownload(item.ID, item.WorkshopID)
	if err != nil {
		m.setError(item, "Failed to prepare version: "+err.Error())
		return
	}
	item.generation = gen

	progressChan := make(chan depot.ProgressUpdate, 100)
	var dlErr error
	go func() {
		_, dlErr = depot.DownloadWithProgress(depot.Options{
			Downloader:          config.Downloader,
			WorkshopID:          item.WorkshopID,
			Username:            config.Username,
			Password:            config.Password,
			OutputDir:           contentver.StagingRoot(),
			DepotDownloaderPath: config.DepotDownloaderPath,
			SteamCmdPath:        config.SteamCmdPath,
		}, progressChan)
		close(progressChan)
	}()

	for update := range progressChan {
		m.mu.Lock()
		if update.Message != "" {
			item.Message = update.Message
		}
		if update.Percentage > 0 {
			item.Percentage = update.Percentage
		}
		if update.FileName != "" {
			item.FileName = update.FileName
		}
		m.mu.Unlock()
		m.broadcast()
	}

	if dlErr != nil {
		log.Printf("[QUEUE] download failed for %s: %v", item.Name, dlErr)
		contentver.AbortModDownload(item.ID, item.WorkshopID, item.generation)
		m.setError(item, "Download failed: "+dlErr.Error())
		return
	}

	if err := contentver.FinalizeModDownload(item.ID, item.WorkshopID, item.generation); err != nil {
		log.Printf("[QUEUE] failed to finalize mod %s: %v", item.WorkshopID, err)
		contentver.AbortModDownload(item.ID, item.WorkshopID, item.generation)
		m.setError(item, "Failed to finalize download: "+err.Error())
		return
	}
	server.RescanKeysForMod(item.ID)
	server.RefreshModSignature(item.ID)

	m.mu.Lock()
	item.Status = StatusDone
	item.Percentage = 100
	item.Message = "Download complete"
	m.mu.Unlock()
}

func (m *Manager) setError(item *Item, msg string) {
	m.mu.Lock()
	item.Status = StatusError
	item.Message = msg
	m.mu.Unlock()
}

// Snapshot returns the current queue state.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Item, 0, len(m.items))
	done, active := 0, 0
	for _, it := range m.items {
		items = append(items, *it)
		if it.Status == StatusDone || it.Status == StatusError {
			done++
		}
		if it.Status == StatusDownloading {
			active = it.ID
		}
	}
	return Snapshot{
		Processing: m.processing,
		Active:     active,
		Total:      len(m.items),
		Done:       done,
		Items:      items,
	}
}

// Subscribe registers a new subscriber and immediately delivers the current
// snapshot. The returned channel is closed by Unsubscribe.
func (m *Manager) Subscribe() chan Snapshot {
	ch := make(chan Snapshot, 4)
	m.subMu.Lock()
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	ch <- m.Snapshot()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (m *Manager) Unsubscribe(ch chan Snapshot) {
	m.subMu.Lock()
	if _, ok := m.subs[ch]; ok {
		delete(m.subs, ch)
		close(ch)
	}
	m.subMu.Unlock()
}

func (m *Manager) broadcast() {
	snap := m.Snapshot()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for ch := range m.subs {
		// Snapshots are full-state, so if a subscriber is behind we drop the
		// stale value and deliver the latest.
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
