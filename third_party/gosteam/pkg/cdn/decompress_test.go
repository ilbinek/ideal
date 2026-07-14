package cdn

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// buildVZstd wraps a zstd frame in Steam's "VSZa"/"zsv" container.
func buildVZstd(t *testing.T, orig []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := enc.EncodeAll(orig, nil)
	enc.Close()

	var b bytes.Buffer
	b.WriteString("VSZa")
	binary.Write(&b, binary.LittleEndian, uint32(0)) // crc (unused by decoder)
	b.Write(frame)
	binary.Write(&b, binary.LittleEndian, uint32(0))         // crc footer
	binary.Write(&b, binary.LittleEndian, uint32(len(orig))) // decompressed size
	binary.Write(&b, binary.LittleEndian, uint32(0))         // reserved
	b.WriteString("zsv")
	return b.Bytes()
}

func TestDecompressVZstd(t *testing.T) {
	orig := bytes.Repeat([]byte("gosteam arma3 workshop chunk! "), 64)
	out, err := decompressChunk(buildVZstd(t, orig))
	if err != nil {
		t.Fatalf("decompressChunk: %v", err)
	}
	if !bytes.Equal(out, orig) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(out), len(orig))
	}
}

func TestDecompressUnknownMagic(t *testing.T) {
	if _, err := decompressChunk(bytes.Repeat([]byte{0xAB}, 32)); err == nil {
		t.Fatal("expected error for unknown compression magic")
	}
}

func TestDecompressPKZip(t *testing.T) {
	orig := bytes.Repeat([]byte("pkzip steam depot chunk payload "), 32)

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("chunk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(orig); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := zbuf.Bytes()
	if raw[0] != 'P' || raw[1] != 'K' {
		t.Fatalf("expected PK magic, got %#x", raw[:2])
	}

	out, err := decompressChunk(raw)
	if err != nil {
		t.Fatalf("decompressChunk: %v", err)
	}
	if !bytes.Equal(out, orig) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(out), len(orig))
	}
}
