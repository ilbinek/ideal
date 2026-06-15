package sync

import (
	"github.com/tbdevent/gosteam/pkg/manifest"
)

// reuseOp says a new chunk's content can likely be obtained from existing
// on-disk data at SrcOffset (the offset of the matching chunk in the old file,
// or the chunk's own offset when there is no old manifest). The executor still
// validates the bytes (Adler-32) before trusting a reuse and downloads on miss.
type reuseOp struct {
	Chunk     manifest.Chunk
	SrcOffset uint64
}

// filePlan is the intended delta for one file.
type filePlan struct {
	// Unchanged is true when the file's content hash matches the old manifest
	// and verifyAll is off, so nothing needs to be read, copied, or downloaded.
	Unchanged bool
	Reuse     []reuseOp
	Needed    []manifest.Chunk
}

// planFile decides which of a new file's chunks can be reused from existing data
// and which must be downloaded. It is pure (no I/O) so the delta logic can be
// unit-tested; the executor performs the actual reads, validation, and writes.
//
// Ported from DepotDownloader's DownloadSteam3AsyncDepotFile chunk matching,
// which keys chunks by their SHA-1 ChunkID so chunks may move within a file
// between versions.
func planFile(nf manifest.File, of *manifest.File, fileExists, verifyAll bool) filePlan {
	if !fileExists {
		// Nothing on disk: every chunk must be downloaded.
		return filePlan{Needed: append([]manifest.Chunk(nil), nf.Chunks...)}
	}

	if of != nil && shaEqual(of.SHA, nf.SHA) && !verifyAll {
		return filePlan{Unchanged: true}
	}

	var plan filePlan
	if of != nil {
		oldByID := make(map[string]manifest.Chunk, len(of.Chunks))
		for _, c := range of.Chunks {
			oldByID[string(c.ID)] = c
		}
		for _, nc := range nf.Chunks {
			if oc, ok := oldByID[string(nc.ID)]; ok {
				plan.Reuse = append(plan.Reuse, reuseOp{Chunk: nc, SrcOffset: oc.Offset})
			} else {
				plan.Needed = append(plan.Needed, nc)
			}
		}
		return plan
	}

	// File exists but we have no previous manifest for it: try to reuse the
	// bytes already at each chunk's own offset (validated by the executor),
	// falling back to download on mismatch.
	for _, nc := range nf.Chunks {
		plan.Reuse = append(plan.Reuse, reuseOp{Chunk: nc, SrcOffset: nc.Offset})
	}
	return plan
}

// prunable returns the names of files present in the old manifest but absent
// from the new one. These were deleted on the remote and should be removed
// locally. Mirrors DepotDownloader's file-deletion-on-update behavior.
func prunable(newM, oldM *manifest.Manifest) []string {
	if oldM == nil {
		return nil
	}
	keep := make(map[string]bool, len(newM.Files))
	for i := range newM.Files {
		keep[newM.Files[i].Name] = true
	}
	var out []string
	for i := range oldM.Files {
		f := &oldM.Files[i]
		if f.IsDirectory() {
			continue
		}
		if !keep[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out
}

func shaEqual(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
