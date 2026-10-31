package utils

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeKeyBlob builds a .bikey-style header: authority\0 + len + PUBLICKEYBLOB.
// The same layout prefixes .bisign files, so it is used for both.
func fakeKeyBlob(authority string, modulusSeed byte) []byte {
	const bits = 1024
	var buf bytes.Buffer
	buf.WriteString(authority)
	buf.WriteByte(0)
	blob := new(bytes.Buffer)
	blob.Write([]byte{0x06, 0x02, 0x00, 0x00})              // bType, bVersion, reserved
	binary.Write(blob, binary.LittleEndian, uint32(0x2400)) // CALG_RSA_SIGN
	blob.WriteString("RSA1")
	binary.Write(blob, binary.LittleEndian, uint32(bits))
	binary.Write(blob, binary.LittleEndian, uint32(65537))
	mod := make([]byte, bits/8)
	for i := range mod {
		mod[i] = modulusSeed + byte(i)
	}
	blob.Write(mod)
	binary.Write(&buf, binary.LittleEndian, uint32(blob.Len()))
	buf.Write(blob.Bytes())
	return buf.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func makeMod(t *testing.T, pbos []string, signed map[string]string, keys map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range pbos {
		writeFile(t, filepath.Join(dir, "Addons", p), []byte("pbo"))
		if auth, ok := signed[p]; ok {
			writeFile(t, filepath.Join(dir, "Addons", p+"."+auth+".bisign"), fakeKeyBlob(auth, 1))
		}
	}
	for name, data := range keys {
		writeFile(t, filepath.Join(dir, "Keys", name), data)
	}
	return dir
}

func TestCheckModSignatures_AllSigned(t *testing.T) {
	dir := makeMod(t,
		[]string{"a.pbo", "B.pbo"},
		map[string]string{"a.pbo": "mymod_1.0", "B.pbo": "mymod_1.0"},
		map[string][]byte{"mymod_1.0.bikey": fakeKeyBlob("mymod_1.0", 1)},
	)
	r := CheckModSignatures(dir, nil)
	if r.Status != SignatureOK || r.PboCount != 2 || r.SignedCount != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckModSignatures_KeyFilenameDiffers(t *testing.T) {
	dir := makeMod(t,
		[]string{"a.pbo"},
		map[string]string{"a.pbo": "mymod_1.0"},
		map[string][]byte{"SomethingElse.BIKEY": fakeKeyBlob("mymod_1.0", 1)},
	)
	if r := CheckModSignatures(dir, nil); r.Status != SignatureOK {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckModSignatures_MissingKey(t *testing.T) {
	dir := makeMod(t, []string{"a.pbo"}, map[string]string{"a.pbo": "mymod_1.0"}, nil)
	r := CheckModSignatures(dir, nil)
	if r.Status != SignatureMissingKey || len(r.MissingKeys) != 1 || r.MissingKeys[0] != "mymod_1.0" {
		t.Fatalf("got %+v", r)
	}
	// Key available from the server's key set instead.
	extra := KeySet{}
	extra.Add(KeyID{Authority: "mymod_1.0"})
	if r := CheckModSignatures(dir, extra); r.Status != SignatureOK {
		t.Fatalf("with extra keys got %+v", r)
	}
}

func TestCheckModSignatures_WrongKeyModulus(t *testing.T) {
	// Same authority name, different public key: signatures do not match.
	dir := makeMod(t,
		[]string{"a.pbo"},
		map[string]string{"a.pbo": "mymod_1.0"},
		map[string][]byte{"mymod_1.0.bikey": fakeKeyBlob("mymod_1.0", 99)},
	)
	if r := CheckModSignatures(dir, nil); r.Status != SignatureMissingKey {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckModSignatures_Unsigned(t *testing.T) {
	dir := makeMod(t,
		[]string{"a.pbo", "b.pbo"},
		map[string]string{"a.pbo": "mymod_1.0"},
		map[string][]byte{"mymod_1.0.bikey": fakeKeyBlob("mymod_1.0", 1)},
	)
	r := CheckModSignatures(dir, nil)
	if r.Status != SignatureUnsigned || r.SignedCount != 1 || len(r.UnsignedPbos) != 1 || r.UnsignedPbos[0] != "b.pbo" {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckModSignatures_PrefixNotConfused(t *testing.T) {
	// "a.pbo" must not be considered signed by "a_extra.pbo.x.bisign".
	dir := makeMod(t,
		[]string{"a.pbo", "a_extra.pbo"},
		map[string]string{"a_extra.pbo": "x"},
		map[string][]byte{"x.bikey": fakeKeyBlob("x", 1)},
	)
	r := CheckModSignatures(dir, nil)
	if r.Status != SignatureUnsigned || len(r.UnsignedPbos) != 1 || r.UnsignedPbos[0] != "a.pbo" {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckModSignatures_NoPbosAndMissingDir(t *testing.T) {
	if r := CheckModSignatures(t.TempDir(), nil); r.Status != SignatureNoPbos {
		t.Fatalf("empty: got %+v", r)
	}
	if r := CheckModSignatures(filepath.Join(t.TempDir(), "nope"), nil); r.Status != SignatureUnknown {
		t.Fatalf("missing: got %+v", r)
	}
}
