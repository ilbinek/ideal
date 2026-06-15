package sync

import (
	"bytes"
	"context"
	"crypto/sha1"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tbdevent/gosteam/pkg/manifest"
)

// fakeCDN serves chunk content from an in-memory registry keyed by chunk id.
type fakeCDN struct {
	data  map[string][]byte
	calls int64
}

func (f *fakeCDN) DownloadChunk(_ context.Context, _, _ uint32, c manifest.Chunk, _ []byte) ([]byte, error) {
	atomic.AddInt64(&f.calls, 1)
	return f.data[string(c.ID)], nil
}

// mkFile builds a manifest.File (and registers its chunk content with cdn) from
// a list of chunk byte-slices.
func mkFile(cdn *fakeCDN, name string, chunks ...[]byte) manifest.File {
	f := manifest.File{Name: name}
	var off uint64
	var all []byte
	for _, c := range chunks {
		id := sha1.Sum(c)
		f.Chunks = append(f.Chunks, manifest.Chunk{
			ID:        id[:],
			CRC:       manifest.Adler32(c),
			Offset:    off,
			UncompLen: uint32(len(c)),
			CompLen:   uint32(len(c)),
		})
		if cdn != nil {
			cdn.data[string(id[:])] = c
		}
		off += uint64(len(c))
		all = append(all, c...)
	}
	f.Size = off
	sum := sha1.Sum(all)
	f.SHA = sum[:]
	return f
}

func read(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func TestSyncDelta(t *testing.T) {
	dir := t.TempDir()
	A := bytes.Repeat([]byte("A"), 100)
	B := bytes.Repeat([]byte("B"), 100)
	C := bytes.Repeat([]byte("C"), 100)

	cdn := &fakeCDN{data: map[string][]byte{}}
	syncer := NewSyncer(cdn, Options{MaxDownloads: 4, Prune: true})

	// v1: fresh install of a.txt = A+B and b.txt = C.
	v1 := &manifest.Manifest{DepotID: 1, GID: 1, Files: []manifest.File{
		mkFile(cdn, "a.txt", A, B),
		mkFile(cdn, "b.txt", C),
	}}
	st, err := syncer.Sync(context.Background(), Target{New: v1, OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.FilesWritten != 2 || st.ChunksDownloaded != 3 {
		t.Fatalf("v1 stats = %+v; want 2 files, 3 chunks downloaded", st)
	}
	if got := read(t, dir, "a.txt"); !bytes.Equal(got, append(append([]byte{}, A...), B...)) {
		t.Fatal("a.txt content wrong after v1")
	}

	// v2: identical manifest, previous installed — everything unchanged.
	cdn.calls = 0
	st, err = syncer.Sync(context.Background(), Target{New: v1, Old: v1, OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.FilesUnchanged != 2 || st.ChunksDownloaded != 0 || cdn.calls != 0 {
		t.Fatalf("v2 stats = %+v, calls=%d; want 2 unchanged, 0 downloads", st, cdn.calls)
	}

	// v3: a.txt changes B->C (reuse A, download the new chunk); b.txt removed (pruned).
	cdn.calls = 0
	v3 := &manifest.Manifest{DepotID: 1, GID: 2, Files: []manifest.File{
		mkFile(cdn, "a.txt", A, C),
	}}
	st, err = syncer.Sync(context.Background(), Target{New: v3, Old: v1, OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.ChunksReused != 1 {
		t.Fatalf("v3 reused %d chunks; want 1 (chunk A)", st.ChunksReused)
	}
	if st.ChunksDownloaded != 1 {
		t.Fatalf("v3 downloaded %d chunks; want 1 (new chunk C)", st.ChunksDownloaded)
	}
	if st.FilesDeleted != 1 {
		t.Fatalf("v3 deleted %d files; want 1 (b.txt)", st.FilesDeleted)
	}
	if got := read(t, dir, "a.txt"); !bytes.Equal(got, append(append([]byte{}, A...), C...)) {
		t.Fatal("a.txt content wrong after v3")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("b.txt should have been pruned")
	}
}

func TestSyncVerifyAllRepairsCorruption(t *testing.T) {
	dir := t.TempDir()
	A := bytes.Repeat([]byte("A"), 100)
	B := bytes.Repeat([]byte("B"), 100)

	cdn := &fakeCDN{data: map[string][]byte{}}
	m := &manifest.Manifest{DepotID: 1, GID: 1, Files: []manifest.File{mkFile(cdn, "a.txt", A, B)}}

	syncer := NewSyncer(cdn, Options{MaxDownloads: 2})
	if _, err := syncer.Sync(context.Background(), Target{New: m, OutDir: dir}); err != nil {
		t.Fatal(err)
	}

	// Corrupt the file on disk, then re-sync with VerifyAll against the same manifest.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), bytes.Repeat([]byte("X"), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	verifier := NewSyncer(cdn, Options{MaxDownloads: 2, VerifyAll: true})
	st, err := verifier.Sync(context.Background(), Target{New: m, Old: m, OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.ChunksDownloaded != 2 {
		t.Fatalf("verify-all re-downloaded %d chunks; want 2 (corruption detected)", st.ChunksDownloaded)
	}
	if got := read(t, dir, "a.txt"); !bytes.Equal(got, append(append([]byte{}, A...), B...)) {
		t.Fatal("a.txt not repaired by verify-all")
	}
}
