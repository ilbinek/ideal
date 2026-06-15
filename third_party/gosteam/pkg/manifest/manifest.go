// Package manifest provides a clean in-memory model of a Steam depot manifest
// (the file -> chunk mapping that drives downloading) plus an on-disk cache so a
// previously downloaded manifest can be diffed against on the next run.
//
// A depot manifest is a zip whose single entry is a sequence of length-prefixed,
// magic-framed protobuf blocks: a ContentManifestPayload (files + chunks), a
// ContentManifestMetadata (depot id, gid, filename-encryption flag), an optional
// signature, and an end marker. Filenames may be AES-encrypted with the depot
// key. This mirrors SteamKit2's DepotManifest.Deserialize / DecryptFilenames.
package manifest

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/0xAozora/go-steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"

	"github.com/tbdevent/gosteam/pkg/crypto"
)

// Block magic markers (little-endian uint32 prefixes).
const (
	magicPayload       = 0x71F617D0
	magicMetadata      = 0x1F4812BE
	magicSignature     = 0x1B81B817
	magicEndOfManifest = 0x32C415AB
)

// EDepotFileFlag bits we care about (subset of SteamKit2's EDepotFileFlag).
const (
	FlagDirectory  = 1 << 6 // 64
	FlagSymlink    = 1 << 9 // 512
	FlagExecutable = 1 << 3 // 8
)

// Chunk is one ~1MB content-addressed piece of a file.
type Chunk struct {
	ID        []byte // SHA-1 of the (uncompressed) chunk; identifies it on the CDN
	CRC       uint32 // Adler-32 of the uncompressed chunk
	Offset    uint64 // byte offset within the file
	CompLen   uint32 // compressed length as stored on the CDN
	UncompLen uint32 // uncompressed length
}

// File is one entry in a manifest.
type File struct {
	Name       string
	Flags      uint32
	Size       uint64
	SHA        []byte // SHA-1 of the whole file's content
	LinkTarget string // populated for symlinks
	Chunks     []Chunk
}

// IsDirectory reports whether the entry is a directory placeholder.
func (f *File) IsDirectory() bool { return f.Flags&FlagDirectory != 0 }

// IsSymlink reports whether the entry is a symbolic link.
func (f *File) IsSymlink() bool { return f.Flags&FlagSymlink != 0 }

// Manifest is a parsed, decrypted depot manifest.
type Manifest struct {
	DepotID            uint32
	GID                uint64 // manifest id
	CreationTime       uint32
	FilenamesEncrypted bool
	Files              []File
}

// Parse decodes a raw manifest payload (the bytes downloaded from the CDN,
// already AES-decrypted). If filenames are encrypted, depotKey must be the
// 32-byte depot decryption key; pass nil if you know filenames are clear.
func Parse(raw []byte, depotKey []byte) (*Manifest, error) {
	inner, err := unzip(raw)
	if err != nil {
		return nil, err
	}

	var payload protobuf.ContentManifestPayload
	var meta protobuf.ContentManifestMetadata
	if err := decodeBlocks(inner, &payload, &meta); err != nil {
		return nil, err
	}

	m := &Manifest{
		DepotID:            meta.GetDepotId(),
		GID:                meta.GetGidManifest(),
		CreationTime:       meta.GetCreationTime(),
		FilenamesEncrypted: meta.GetFilenamesEncrypted(),
	}

	if m.FilenamesEncrypted && depotKey == nil {
		return nil, errors.New("manifest: filenames are encrypted but no depot key was provided")
	}

	m.Files = make([]File, 0, len(payload.GetMappings()))
	for _, fm := range payload.GetMappings() {
		name := fm.GetFilename()
		if m.FilenamesEncrypted {
			name, err = decryptFilename(name, depotKey)
			if err != nil {
				return nil, err
			}
		}
		// Steam uses backslash path separators; normalize to forward slashes.
		name = normalizeName(name)

		f := File{
			Name:       name,
			Flags:      fm.GetFlags(),
			Size:       fm.GetSize(),
			SHA:        fm.GetShaContent(),
			LinkTarget: normalizeName(fm.GetLinktarget()),
		}
		f.Chunks = make([]Chunk, 0, len(fm.GetChunks()))
		for _, c := range fm.GetChunks() {
			f.Chunks = append(f.Chunks, Chunk{
				ID:        c.GetSha(),
				CRC:       c.GetCrc(),
				Offset:    c.GetOffset(),
				CompLen:   c.GetCbCompressed(),
				UncompLen: c.GetCbOriginal(),
			})
		}
		// Chunks within a file are ordered by offset so we can write/copy sequentially.
		sort.Slice(f.Chunks, func(i, j int) bool { return f.Chunks[i].Offset < f.Chunks[j].Offset })
		m.Files = append(m.Files, f)
	}

	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Name < m.Files[j].Name })
	return m, nil
}

// unzip returns the single inner entry of a manifest zip. Steam wraps the
// magic-framed blocks in a zip with one file.
func unzip(raw []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("manifest: open zip: %w", err)
	}
	if len(zr.File) == 0 {
		return nil, errors.New("manifest: empty zip")
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		return nil, fmt.Errorf("manifest: open zip entry: %w", err)
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func decodeBlocks(b []byte, payload *protobuf.ContentManifestPayload, meta *protobuf.ContentManifestMetadata) error {
	for len(b) >= 8 {
		magic := binary.LittleEndian.Uint32(b)
		if magic == magicEndOfManifest {
			return nil
		}
		l := binary.LittleEndian.Uint32(b[4:])
		if int(l)+8 > len(b) {
			return errors.New("manifest: truncated block")
		}
		block := b[8 : 8+l]
		switch magic {
		case magicPayload:
			if err := proto.Unmarshal(block, payload); err != nil {
				return fmt.Errorf("manifest: payload: %w", err)
			}
		case magicMetadata:
			if err := proto.Unmarshal(block, meta); err != nil {
				return fmt.Errorf("manifest: metadata: %w", err)
			}
		case magicSignature:
			// ignored
		default:
			return fmt.Errorf("manifest: unknown block magic %#x", magic)
		}
		b = b[8+l:]
	}
	return nil
}

// decryptFilename decrypts a base64-encoded, AES-256 encrypted filename using the depot key.
func decryptFilename(encoded string, depotKey []byte) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("manifest: filename base64: %w", err)
	}
	pt, err := crypto.SymmetricDecrypt(depotKey, ct)
	if err != nil {
		return "", fmt.Errorf("manifest: decrypt filename: %w", err)
	}
	// Decrypted filenames are null-terminated.
	pt = bytes.TrimRight(pt, "\x00")
	return string(pt), nil
}

func normalizeName(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			out[i] = '/'
		} else {
			out[i] = s[i]
		}
	}
	return string(out)
}

// --- on-disk cache ----------------------------------------------------------

// Save writes the raw (decrypted, pre-zip-removal) manifest bytes to path along
// with a .sha sidecar for integrity, so the manifest can be reloaded next run.
func Save(path string, raw []byte) error {
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	sum := sha1.Sum(raw)
	return os.WriteFile(path+".sha", sum[:], 0o644)
}

// Load reads a cached manifest blob from path, verifies its .sha sidecar, and
// parses it. Returns (nil, nil) if the file does not exist.
func Load(path string, depotKey []byte) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if want, err := os.ReadFile(path + ".sha"); err == nil {
		got := sha1.Sum(raw)
		if !bytes.Equal(want, got[:]) {
			return nil, fmt.Errorf("manifest: cache checksum mismatch for %s", path)
		}
	}
	return Parse(raw, depotKey)
}
