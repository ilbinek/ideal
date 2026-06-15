package manifest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/0xAozora/go-steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

// buildManifest serializes a payload+metadata into Steam's magic-framed, zipped
// manifest layout so we can exercise Parse without hitting the network.
func buildManifest(t *testing.T, payload *protobuf.ContentManifestPayload, meta *protobuf.ContentManifestMetadata) []byte {
	t.Helper()

	var inner bytes.Buffer
	writeBlock := func(magic uint32, m proto.Message) {
		b, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		binary.Write(&inner, binary.LittleEndian, magic)
		binary.Write(&inner, binary.LittleEndian, uint32(len(b)))
		inner.Write(b)
	}
	writeBlock(magicPayload, payload)
	writeBlock(magicMetadata, meta)
	binary.Write(&inner, binary.LittleEndian, uint32(magicEndOfManifest))

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("z")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(inner.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zbuf.Bytes()
}

func TestParse(t *testing.T) {
	payload := &protobuf.ContentManifestPayload{
		Mappings: []*protobuf.ContentManifestPayload_FileMapping{
			{
				Filename:   proto.String(`addons\main.pbo`),
				Size:       proto.Uint64(2048),
				Flags:      proto.Uint32(0),
				ShaContent: bytes.Repeat([]byte{0x01}, 20),
				Chunks: []*protobuf.ContentManifestPayload_FileMapping_ChunkData{
					{Sha: bytes.Repeat([]byte{0xBB}, 20), Crc: proto.Uint32(2), Offset: proto.Uint64(1024), CbOriginal: proto.Uint32(1024), CbCompressed: proto.Uint32(300)},
					{Sha: bytes.Repeat([]byte{0xAA}, 20), Crc: proto.Uint32(1), Offset: proto.Uint64(0), CbOriginal: proto.Uint32(1024), CbCompressed: proto.Uint32(256)},
				},
			},
			{
				Filename: proto.String(`addons`),
				Flags:    proto.Uint32(FlagDirectory),
			},
		},
	}
	meta := &protobuf.ContentManifestMetadata{
		DepotId:            proto.Uint32(107411),
		GidManifest:        proto.Uint64(123456789),
		FilenamesEncrypted: proto.Bool(false),
	}

	m, err := Parse(buildManifest(t, payload, meta), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.DepotID != 107411 || m.GID != 123456789 {
		t.Fatalf("metadata mismatch: depot=%d gid=%d", m.DepotID, m.GID)
	}
	if len(m.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(m.Files))
	}
	// Files sorted by name: "addons" before "addons/main.pbo".
	if m.Files[0].Name != "addons" || !m.Files[0].IsDirectory() {
		t.Fatalf("file[0] = %+v, want directory 'addons'", m.Files[0])
	}
	f := m.Files[1]
	if f.Name != "addons/main.pbo" {
		t.Fatalf("file[1] name = %q, want addons/main.pbo (backslash normalized)", f.Name)
	}
	// Chunks must be sorted by offset.
	if len(f.Chunks) != 2 || f.Chunks[0].Offset != 0 || f.Chunks[1].Offset != 1024 {
		t.Fatalf("chunks not sorted by offset: %+v", f.Chunks)
	}
}
