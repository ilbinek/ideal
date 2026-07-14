package cdn

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/itchio/lzma"
	"github.com/klauspost/compress/zstd"
)

// decompressChunk decompresses a decrypted depot chunk. Steam wraps chunk data
// in one of three containers, identified by the first bytes: VZstd ("VSZa",
// zstd), VZip ("VZa", LZMA), or a raw zip ("PK\x03\x04"). Ported from
// SteamKit2's DepotChunk.Process + VZstdUtil/VZipUtil/ZipUtil.
func decompressChunk(data []byte) ([]byte, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("cdn: decrypted chunk too small (%d bytes)", len(data))
	}
	switch {
	case data[0] == 'V' && data[1] == 'S' && data[2] == 'Z' && data[3] == 'a':
		return decompressVZstd(data)
	case data[0] == 'V' && data[1] == 'Z' && data[2] == 'a':
		return decompressVZip(data)
	case data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04:
		return decompressPKZip(data)
	default:
		return nil, fmt.Errorf("cdn: unknown chunk compression (first bytes %#x)", data[:4])
	}
}

var zstdDec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))

// decompressVZstd handles the modern zstd container:
//
//	[ "VSZa" (4) ][ crc32 (4) ][ zstd frame ... ][ crc32 (4) ][ size (4) ][ ? (4) ][ "zsv" (3) ]
//
// The trailing 15 bytes are the footer; the zstd frame is everything between.
func decompressVZstd(b []byte) ([]byte, error) {
	const footerLen = 15
	if len(b) < 8+footerLen {
		return nil, fmt.Errorf("cdn: vzstd too small (%d bytes)", len(b))
	}
	if b[len(b)-3] != 'z' || b[len(b)-2] != 's' || b[len(b)-1] != 'v' {
		return nil, fmt.Errorf("cdn: bad vzstd footer")
	}
	sizeDecompressed := binary.LittleEndian.Uint32(b[len(b)-11:])
	frame := b[8 : len(b)-footerLen]

	out, err := zstdDec.DecodeAll(frame, make([]byte, 0, sizeDecompressed))
	if err != nil {
		return nil, fmt.Errorf("cdn: zstd decode: %w", err)
	}
	if uint32(len(out)) != sizeDecompressed {
		return nil, fmt.Errorf("cdn: zstd size mismatch (got %d, want %d)", len(out), sizeDecompressed)
	}
	return out, nil
}

// decompressVZip handles the legacy LZMA container:
//
//	[ "VZ" (2) ][ ver 'a' (1) ][ ts/crc (4) ][ lzma props (5) ][ data ... ][ crc (4) ][ size (4) ][ "zv" (2) ]
//
// It reconstructs a standard .lzma stream (5 prop bytes + 8-byte size + data)
// and feeds it to the LZMA reader. Mirrors the vendored fork's decompressVZip.
func decompressVZip(vz []byte) ([]byte, error) {
	const (
		headerLen = 7  // "VZ" + version + ts/crc
		footerLen = 10 // crc + size + "zv"
	)
	if len(vz) < headerLen+5+footerLen {
		return nil, fmt.Errorf("cdn: vzip too small (%d bytes)", len(vz))
	}
	if vz[2] != 'a' {
		return nil, fmt.Errorf("cdn: unsupported vzip version %q", vz[2])
	}
	if footer := binary.LittleEndian.Uint16(vz[len(vz)-2:]); footer != 0x767A { // "zv"
		return nil, fmt.Errorf("cdn: bad vzip footer %#x", footer)
	}

	l := len(vz)
	sizeDecompressed := binary.LittleEndian.Uint32(vz[l-6 : l-2])

	// Build a .lzma stream: [5 props][8-byte uncompressed size LE][compressed data].
	stream := make([]byte, l-headerLen-footerLen+5+8)
	copy(stream[0:5], vz[headerLen:headerLen+5])                          // props
	binary.LittleEndian.PutUint64(stream[5:13], uint64(sizeDecompressed)) // size
	copy(stream[13:], vz[headerLen+5:l-footerLen])                        // data

	out, err := io.ReadAll(lzma.NewReader(bytes.NewReader(stream)))
	if err != nil {
		return nil, fmt.Errorf("cdn: lzma decode: %w", err)
	}
	if uint32(len(out)) != sizeDecompressed {
		return nil, fmt.Errorf("cdn: lzma size mismatch (got %d, want %d)", len(out), sizeDecompressed)
	}
	return out, nil
}

// decompressPKZip handles the oldest chunk container: a standard ZIP archive
// with a single entry (SteamKit2 ZipUtil.Decompress).
func decompressPKZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("cdn: pkzip open: %w", err)
	}
	if len(zr.File) == 0 {
		return nil, fmt.Errorf("cdn: pkzip has no entries")
	}
	f, err := zr.File[0].Open()
	if err != nil {
		return nil, fmt.Errorf("cdn: pkzip entry: %w", err)
	}
	defer f.Close()
	out, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("cdn: pkzip read: %w", err)
	}
	return out, nil
}
