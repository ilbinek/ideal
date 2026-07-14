package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"

	"github.com/tbdevent/gosteam/pkg/downloader"
)

// progressUI renders an aggregate, live progress bar for a concurrent bulk
// download, printing a permanent line as each item finishes. It consumes
// downloader.Progress / AppProgress events (which arrive from multiple goroutines).
type progressUI struct {
	mu       sync.Mutex
	items    map[string]*itemProgress
	order    []string
	expected int  // total items, set before downloading
	tty      bool // whether stdout is a terminal (animate) or not (plain lines)
	label    string
}

type itemProgress struct {
	title     string
	completed uint64
	total     uint64
	done      bool
	failed    bool
}

func newProgressUI() *progressUI {
	return &progressUI{
		items: map[string]*itemProgress{},
		tty:   term.IsTerminal(int(os.Stdout.Fd())),
		label: "mods",
	}
}

func (u *progressUI) handle(p downloader.Progress) {
	u.mu.Lock()
	defer u.mu.Unlock()

	key := fmt.Sprint(p.Item.PubFileID)
	it := u.items[key]
	if it == nil {
		it = &itemProgress{title: p.Item.Title}
		u.items[key] = it
		u.order = append(u.order, key)
	}

	switch p.Phase {
	case downloader.PhaseDownloading:
		it.completed, it.total = p.CompletedBytes, p.TotalBytes
	case downloader.PhaseCompleted:
		it.completed, it.total, it.done = p.TotalBytes, p.TotalBytes, true
		s := p.Stats
		u.printLine(fmt.Sprintf("OK   %d %q: %d downloaded, %d reused, %d written, %d unchanged, %d deleted",
			p.Item.PubFileID, p.Item.Title,
			s.ChunksDownloaded, s.ChunksReused, s.FilesWritten, s.FilesUnchanged, s.FilesDeleted))
	case downloader.PhaseFailed:
		it.failed = true
		u.printLine(fmt.Sprintf("FAIL %d %q: %v", p.Item.PubFileID, p.Item.Title, p.Err))
	}
	u.render()
}

func (u *progressUI) handleApp(p downloader.AppProgress) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.label = "depots"

	key := downloader.FormatDepotKey(p.Depot)
	it := u.items[key]
	if it == nil {
		it = &itemProgress{title: p.Depot.Name}
		u.items[key] = it
		u.order = append(u.order, key)
	}

	switch p.Phase {
	case downloader.PhaseDownloading:
		it.completed, it.total = p.CompletedBytes, p.TotalBytes
	case downloader.PhaseCompleted:
		it.completed, it.total, it.done = p.TotalBytes, p.TotalBytes, true
		s := p.Stats
		u.printLine(fmt.Sprintf("OK   depot %d: %d downloaded, %d reused, %d written, %d unchanged, %d deleted",
			p.Depot.DepotID,
			s.ChunksDownloaded, s.ChunksReused, s.FilesWritten, s.FilesUnchanged, s.FilesDeleted))
	case downloader.PhaseFailed:
		it.failed = true
		u.printLine(fmt.Sprintf("FAIL depot %d: %v", p.Depot.DepotID, p.Err))
	}
	u.render()
}

// finish clears the live bar after the download completes.
func (u *progressUI) finish() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tty {
		fmt.Print("\r\033[K")
	}
}

// printLine emits a permanent line above the live bar (caller holds the lock).
func (u *progressUI) printLine(s string) {
	if u.tty {
		fmt.Print("\r\033[K" + s + "\n")
	} else {
		fmt.Println(s)
	}
}

// render redraws the aggregate bar (caller holds the lock).
func (u *progressUI) render() {
	if !u.tty {
		return
	}
	var sumC, sumT uint64
	done := 0
	for _, id := range u.order {
		it := u.items[id]
		sumC += it.completed
		sumT += it.total
		if it.done || it.failed {
			done++
		}
	}
	total := u.expected
	if total < len(u.order) {
		total = len(u.order)
	}

	pct := 0.0
	if sumT > 0 {
		pct = float64(sumC) / float64(sumT) * 100
	}
	label := u.label
	if label == "" {
		label = "items"
	}
	fmt.Printf("\r\033[K%s %5.1f%%  %d/%d %s  %s/%s",
		bar(pct, 24), pct, done, total, label, humanBytes(sumC), humanBytes(sumT))
}

func bar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}
