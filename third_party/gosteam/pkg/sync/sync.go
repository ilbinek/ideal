// Package sync implements gosteam's delta engine: given a freshly downloaded
// depot manifest and the previously installed one, it downloads only the chunks
// that changed, reuses unchanged chunks from the existing files on disk, and
// prunes files that were removed on the remote. This is the Go port of
// DepotDownloader's DownloadSteam3AsyncDepotFile(s).
package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	gosync "sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/tbdevent/gosteam/pkg/manifest"
)

// ChunkDownloader fetches and verifies a single decrypted, decompressed chunk.
// *cdn.Client satisfies this; tests use a fake.
type ChunkDownloader interface {
	DownloadChunk(ctx context.Context, appID, depotID uint32, chunk manifest.Chunk, depotKey []byte) ([]byte, error)
}

// Options tunes a sync.
type Options struct {
	MaxDownloads int  // max concurrent chunk downloads (per Sync call); default 8
	VerifyAll    bool // re-validate every chunk even if the file hash matches
	Prune        bool // delete files removed on the remote
}

// Stats summarizes what a Sync did.
type Stats struct {
	DownloadedBytes  uint64
	ReusedBytes      uint64
	ChunksDownloaded int
	ChunksReused     int
	FilesWritten     int
	FilesUnchanged   int
	FilesDeleted     int
}

// Target describes one depot/item to sync into a directory.
type Target struct {
	AppID    uint32
	DepotID  uint32
	DepotKey []byte
	New      *manifest.Manifest // the manifest to install
	Old      *manifest.Manifest // previously installed manifest, or nil
	OutDir   string             // destination directory for this item's files

	// OnProgress, if set, is called as bytes are materialized (downloaded or
	// reused), with the running completed-byte count and the total bytes for
	// this target. It may be called concurrently from multiple goroutines and
	// frequently, so callers should keep it cheap (and throttle UI updates).
	OnProgress func(completed, total uint64)
}

// reporter accumulates completed bytes and fans them out to a callback.
type reporter struct {
	total     uint64
	completed atomic.Uint64
	cb        func(completed, total uint64)
}

func (r *reporter) add(n uint64) {
	if r == nil || n == 0 {
		return
	}
	c := r.completed.Add(n)
	if r.cb != nil {
		r.cb(c, r.total)
	}
}

// fileLock is a refcounted mutex so concurrent Sync calls on the same Syncer
// cannot finalize or prune the same output path at once.
type fileLock struct {
	mu   gosync.Mutex
	refs int
}

// Syncer applies manifests to disk using a download worker pool.
type Syncer struct {
	cdn   ChunkDownloader
	sem   chan struct{}
	opts  Options
	mu    gosync.Mutex
	locks map[string]*fileLock
}

// NewSyncer creates a Syncer. A shared Syncer can be used across multiple Sync
// calls so the MaxDownloads limit caps total concurrent downloads.
func NewSyncer(d ChunkDownloader, opts Options) *Syncer {
	if opts.MaxDownloads <= 0 {
		opts.MaxDownloads = 8
	}
	return &Syncer{
		cdn:   d,
		sem:   make(chan struct{}, opts.MaxDownloads),
		opts:  opts,
		locks: make(map[string]*fileLock),
	}
}

func (s *Syncer) lockPath(path string) func() {
	key := filepath.Clean(path)
	s.mu.Lock()
	l, ok := s.locks[key]
	if !ok {
		l = &fileLock{}
		s.locks[key] = l
	}
	l.refs++
	s.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		s.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(s.locks, key)
		}
		s.mu.Unlock()
	}
}

// Sync installs t.New into t.OutDir, reusing data from existing files and from
// t.Old where possible.
func (s *Syncer) Sync(ctx context.Context, t Target) (Stats, error) {
	var st Stats

	oldByName := map[string]*manifest.File{}
	if t.Old != nil {
		for i := range t.Old.Files {
			oldByName[t.Old.Files[i].Name] = &t.Old.Files[i]
		}
	}

	files := append([]manifest.File(nil), t.New.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	rep := &reporter{cb: t.OnProgress}
	for i := range files {
		rep.total += files[i].Size
	}

	for i := range files {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		if err := s.syncFile(ctx, t, &files[i], oldByName[files[i].Name], &st, rep); err != nil {
			return st, err
		}
	}

	if s.opts.Prune {
		if err := s.prune(t, &st); err != nil {
			return st, err
		}
	}
	return st, nil
}

func (s *Syncer) syncFile(ctx context.Context, t Target, nf, of *manifest.File, st *Stats, rep *reporter) error {
	target := filepath.Join(t.OutDir, filepath.FromSlash(nf.Name))

	if nf.IsDirectory() {
		return os.MkdirAll(target, 0o755)
	}
	unlock := s.lockPath(target)
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if nf.IsSymlink() {
		_ = os.Remove(target)
		if err := os.Symlink(filepath.FromSlash(nf.LinkTarget), target); err != nil {
			return fmt.Errorf("sync: symlink %s: %w", nf.Name, err)
		}
		st.FilesWritten++
		return nil
	}

	fileExists := false
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		fileExists = true
	}

	plan := planFile(*nf, of, fileExists, s.opts.VerifyAll)
	if plan.Unchanged {
		st.FilesUnchanged++
		st.ReusedBytes += nf.Size
		rep.add(nf.Size)
		return nil
	}

	// Unique sibling temp so concurrent depots cannot share a .part path.
	stage, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".gosteam.part.*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }() // no-op after a successful rename
	if nf.Size > 0 {
		if err := stage.Truncate(int64(nf.Size)); err != nil {
			stage.Close()
			return err
		}
	}

	var existing *os.File
	if fileExists {
		existing, _ = os.Open(target)
		if existing != nil {
			defer existing.Close()
		}
	}

	// Reuse phase: copy chunks whose existing on-disk bytes still match.
	needed := make([]manifest.Chunk, 0, len(plan.Needed))
	for _, op := range plan.Reuse {
		data := readChunk(existing, op.SrcOffset, op.Chunk.UncompLen)
		if data != nil && manifest.Adler32(data) == op.Chunk.CRC {
			if _, err := stage.WriteAt(data, int64(op.Chunk.Offset)); err != nil {
				stage.Close()
				return err
			}
			st.ReusedBytes += uint64(op.Chunk.UncompLen)
			st.ChunksReused++
			rep.add(uint64(op.Chunk.UncompLen))
		} else {
			needed = append(needed, op.Chunk)
		}
	}
	needed = append(needed, plan.Needed...)

	// Download phase: fetch the remaining chunks concurrently.
	var downloadedBytes, chunksDownloaded int64
	g, gctx := errgroup.WithContext(ctx)
schedule:
	for _, ch := range needed {
		select {
		case s.sem <- struct{}{}:
		case <-gctx.Done():
			break schedule
		}
		g.Go(func() error {
			defer func() { <-s.sem }()
			data, err := s.cdn.DownloadChunk(gctx, t.AppID, t.DepotID, ch, t.DepotKey)
			if err != nil {
				return err
			}
			if _, err := stage.WriteAt(data, int64(ch.Offset)); err != nil {
				return err
			}
			atomic.AddInt64(&downloadedBytes, int64(len(data)))
			atomic.AddInt64(&chunksDownloaded, 1)
			rep.add(uint64(len(data)))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		stage.Close()
		return fmt.Errorf("sync: %s: %w", nf.Name, err)
	}
	st.DownloadedBytes += uint64(downloadedBytes)
	st.ChunksDownloaded += int(chunksDownloaded)

	if err := stage.Close(); err != nil {
		return err
	}
	if existing != nil {
		existing.Close()
	}

	// os.Rename won't overwrite on Windows, so remove the target first.
	_ = os.Remove(target)
	if err := os.Rename(stagePath, target); err != nil {
		return finalizeRenameError(nf.Name, stagePath, target, err)
	}
	if nf.Flags&manifest.FlagExecutable != 0 {
		_ = os.Chmod(target, 0o755)
	}
	st.FilesWritten++
	return nil
}

func finalizeRenameError(name, stagePath, target string, err error) error {
	src := pathStatus(stagePath)
	dst := pathStatus(target)
	parent := pathStatus(filepath.Dir(target))
	return fmt.Errorf("sync: finalize %s: %w (src=%s dest=%s parent=%s)", name, err, src, dst, parent)
}

func pathStatus(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return err.Error()
	}
	return "ok"
}

func (s *Syncer) prune(t Target, st *Stats) error {
	for _, name := range prunable(t.New, t.Old) {
		path := filepath.Join(t.OutDir, filepath.FromSlash(name))
		if err := func() error {
			unlock := s.lockPath(path)
			defer unlock()
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("sync: prune %s: %w", name, err)
			}
			st.FilesDeleted++
			removeEmptyParents(filepath.Dir(path), t.OutDir)
			return nil
		}(); err != nil {
			return err
		}
	}
	return nil
}

// readChunk reads length bytes at offset from f, or returns nil if f is nil or
// too short.
func readChunk(f *os.File, offset uint64, length uint32) []byte {
	if f == nil {
		return nil
	}
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, int64(offset))
	if err != nil || n != int(length) {
		return nil
	}
	return buf
}

// removeEmptyParents removes now-empty directories up to (but not including) root.
func removeEmptyParents(dir, root string) {
	for dir != root && len(dir) > len(root) {
		if err := os.Remove(dir); err != nil {
			return // not empty or not removable; stop
		}
		dir = filepath.Dir(dir)
	}
}
