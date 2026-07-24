package updatecheck

import (
	"fmt"
	"log"
	"sync"
	"time"

	"arma3modmanager/internal/contentver"
	"arma3modmanager/internal/database"
	"arma3modmanager/internal/depot"
	"arma3modmanager/internal/discordhook"
	"arma3modmanager/internal/queue"
	"arma3modmanager/internal/server"
	"arma3modmanager/internal/steamcmdinfo"
	"arma3modmanager/internal/workshop"
)

// ModResult is the update-available flag per mod ID after a check.
type ModResult map[int]bool

// CheckMods compares Workshop time_updated against local state for the given mods.
// It updates last_checked / update_available in the database.
// steamTimeUpdatedByMod maps mod ID → Steam time_updated (for Discord dedup).
func CheckMods(apiKey string, mods []database.Mod) (ModResult, map[int]int64, error) {
	updates := make(ModResult)
	steamTimes := make(map[int]int64)
	if len(mods) == 0 {
		return updates, steamTimes, nil
	}
	if apiKey == "" {
		return nil, nil, fmt.Errorf("Steam API key is not configured")
	}

	workshopIDs := make([]string, 0, len(mods))
	modMap := make(map[string]database.Mod, len(mods))
	for _, mod := range mods {
		workshopIDs = append(workshopIDs, mod.WorkshopID)
		modMap[mod.WorkshopID] = mod
	}

	details, err := workshop.GetModDetails(workshopIDs, apiKey)
	if err != nil {
		return nil, nil, err
	}

	for _, detail := range details {
		if detail.Result != 1 {
			log.Printf("[UPDATECHECK] failed details for workshop ID %s, result %d", detail.PublishedFileID, detail.Result)
			continue
		}
		mod, exists := modMap[detail.PublishedFileID]
		if !exists {
			continue
		}

		lastUpdate := time.Unix(detail.TimeUpdated, 0)
		updateAvailable := false
		if mod.Downloaded && !mod.LastUpdated.IsZero() {
			updateAvailable = lastUpdate.After(mod.LastUpdated)
		} else if !mod.Downloaded {
			updateAvailable = true
		}

		updates[mod.ID] = updateAvailable
		steamTimes[mod.ID] = detail.TimeUpdated
		if err := database.UpdateModChecked(mod.ID, updateAvailable); err != nil {
			log.Printf("[UPDATECHECK] UpdateModChecked(%d): %v", mod.ID, err)
		}
	}
	return updates, steamTimes, nil
}

// CheckBranches compares remote Steam build IDs against installed branch build IDs.
// Returns map of branch ID → remote build ID for branches with an update available.
func CheckBranches(branches []database.ArmaBranch) (map[int]string, error) {
	remote, err := steamcmdinfo.GetDedicatedServerBranches()
	if err != nil {
		return nil, err
	}

	outdated := make(map[int]string)
	for _, b := range branches {
		if !b.Installed {
			_ = database.UpdateBranchChecked(b.ID, false)
			continue
		}

		localBuild := b.BuildID
		if localBuild == "" {
			dir, _, err := contentver.CurrentBranchContentDir(b.ID, b.InstallPath)
			if err != nil || dir == "" {
				dir = b.InstallPath
			}
			if discovered := depot.ReadInstalledBuildID(dir); discovered != "" {
				localBuild = discovered
				_ = database.SetBranchInstalled(b.ID, true, discovered)
				b.BuildID = discovered
			}
		}

		key := steamcmdinfo.NormalizeSteamBranch(b.SteamBranch)
		info, ok := remote[key]
		if !ok || info.BuildID == "" {
			log.Printf("[UPDATECHECK] no remote build for branch %s (%s)", b.Name, key)
			_ = database.UpdateBranchChecked(b.ID, false)
			continue
		}

		// Empty local build (common with gosteam — no appmanifest ACF) is treated as
		// outdated so Discord alerts and auto-download can still fire.
		updateAvailable := localBuild != info.BuildID
		if err := database.UpdateBranchChecked(b.ID, updateAvailable); err != nil {
			log.Printf("[UPDATECHECK] UpdateBranchChecked(%d): %v", b.ID, err)
		}
		if updateAvailable {
			outdated[b.ID] = info.BuildID
		}
	}
	return outdated, nil
}

// NotifyModUpdates sends Discord alerts for mods used by servers that newly need a notify.
func NotifyModUpdates(webhookURL string, mods []database.Mod, updates ModResult, steamTimes map[int]int64) {
	if webhookURL == "" {
		return
	}
	modByID := make(map[int]database.Mod, len(mods))
	for _, m := range mods {
		modByID[m.ID] = m
	}
	for id, available := range updates {
		if !available {
			continue
		}
		mod, ok := modByID[id]
		if !ok {
			continue
		}
		steamTU := steamTimes[id]
		if steamTU == 0 || steamTU == mod.LastNotifiedUpdated {
			continue
		}
		servers, err := database.GetServersUsingMod(id)
		if err != nil || len(servers) == 0 {
			continue
		}
		names := make([]string, len(servers))
		for i, s := range servers {
			names[i] = s.Name
		}
		if err := discordhook.NotifyModUpdate(webhookURL, mod.Name, mod.WorkshopID, names); err != nil {
			log.Printf("[UPDATECHECK] discord mod notify: %v", err)
			continue
		}
		_ = database.SetModNotified(id, steamTU)
	}
}

// NotifyBranchUpdates sends Discord alerts for branches used by servers that newly need a notify.
func NotifyBranchUpdates(webhookURL string, branches []database.ArmaBranch, outdated map[int]string) {
	if webhookURL == "" {
		return
	}
	branchByID := make(map[int]database.ArmaBranch, len(branches))
	for _, b := range branches {
		branchByID[b.ID] = b
	}
	for id, remoteBuild := range outdated {
		b, ok := branchByID[id]
		if !ok {
			continue
		}
		if remoteBuild == "" || remoteBuild == b.LastNotifiedBuildID {
			continue
		}
		servers, err := database.GetServersUsingBranch(id)
		if err != nil || len(servers) == 0 {
			continue
		}
		names := make([]string, len(servers))
		for i, s := range servers {
			names[i] = s.Name
		}
		if err := discordhook.NotifyBranchUpdate(webhookURL, b.Name, b.SteamBranch, remoteBuild, names); err != nil {
			log.Printf("[UPDATECHECK] discord branch notify: %v", err)
			continue
		}
		_ = database.SetBranchNotified(id, remoteBuild)
	}
}

// RunOnce loads config and performs a full mod + branch check with optional Discord notifies.
func RunOnce() {
	cfg, err := database.GetConfig()
	if err != nil || cfg == nil {
		return
	}

	var mods []database.Mod
	var updates ModResult
	if cfg.ApiKey != "" {
		mods, err = database.GetAllMods()
		if err != nil {
			log.Printf("[UPDATECHECK] GetAllMods: %v", err)
		} else {
			var steamTimes map[int]int64
			updates, steamTimes, err = CheckMods(cfg.ApiKey, mods)
			if err != nil {
				log.Printf("[UPDATECHECK] CheckMods: %v", err)
			} else {
				NotifyModUpdates(cfg.DiscordWebhookURL, mods, updates, steamTimes)
			}
		}
	}

	branches, err := database.GetBranches()
	if err != nil {
		log.Printf("[UPDATECHECK] GetBranches: %v", err)
		return
	}
	outdated, err := CheckBranches(branches)
	if err != nil {
		log.Printf("[UPDATECHECK] CheckBranches: %v", err)
		return
	}
	NotifyBranchUpdates(cfg.DiscordWebhookURL, branches, outdated)

	if cfg.AutoDownloadUpdates {
		autoDownload(mods, updates, outdated)
	}
}

// autoDownload enqueues outdated/missing mods and starts branch installs for outdated branches.
// Soft versioning isolates new generations from running servers' pinned content.
func autoDownload(mods []database.Mod, updates ModResult, outdatedBranches map[int]string) {
	if len(mods) > 0 && updates != nil {
		targets := make([]database.Mod, 0)
		modByID := make(map[int]database.Mod, len(mods))
		for _, m := range mods {
			modByID[m.ID] = m
		}
		for id, available := range updates {
			if !available {
				continue
			}
			if m, ok := modByID[id]; ok {
				targets = append(targets, m)
			}
		}
		if len(targets) > 0 {
			added := queue.Default.Enqueue(targets)
			if added > 0 {
				log.Printf("[UPDATECHECK] auto-download enqueued %d mod(s)", added)
			}
		}
	}

	for branchID := range outdatedBranches {
		if err := server.BranchInstallerDefault.Install(branchID); err != nil {
			log.Printf("[UPDATECHECK] auto-download branch %d: %v", branchID, err)
			continue
		}
		log.Printf("[UPDATECHECK] auto-download started branch install %d", branchID)
		// Installer allows only one at a time; remaining branches retry next tick.
		break
	}
}

// Scheduler runs periodic update checks.
type Scheduler struct {
	mu       sync.Mutex
	stopCh   chan struct{}
	running  bool
	interval time.Duration
}

// Default is the process-wide update-check scheduler.
var Default = &Scheduler{}

// Start begins the scheduler (idempotent). Call Reload after settings changes.
func (s *Scheduler) Start() {
	s.Reload()
}

// Reload re-reads interval from config and restarts the ticker loop.
func (s *Scheduler) Reload() {
	cfg, err := database.GetConfig()
	minutes := 0
	if err == nil && cfg != nil {
		minutes = cfg.UpdateCheckIntervalMinutes
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		close(s.stopCh)
		s.running = false
	}

	if minutes <= 0 {
		log.Printf("[UPDATECHECK] auto-check disabled")
		return
	}

	s.interval = time.Duration(minutes) * time.Minute
	s.stopCh = make(chan struct{})
	s.running = true
	stopCh := s.stopCh
	interval := s.interval
	log.Printf("[UPDATECHECK] auto-check every %s", interval)

	go func() {
		// Small delay so startup does not hammer Steam immediately.
		select {
		case <-time.After(30 * time.Second):
		case <-stopCh:
			return
		}
		RunOnce()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				RunOnce()
			case <-stopCh:
				return
			}
		}
	}()
}
