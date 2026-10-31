package utils

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Signature status values reported for a mod.
const (
	SignatureOK         = "ok"          // every PBO signed and a matching bikey exists
	SignatureMissingKey = "missing_key" // every PBO signed, but no matching bikey
	SignatureUnsigned   = "unsigned"    // at least one PBO has no .bisign
	SignatureNoPbos     = "no_pbos"     // nothing to check (no addons/*.pbo)
	SignatureUnknown    = "unknown"     // mod not on disk / unreadable
)

const maxReportedPbos = 25

// SignatureReport summarizes the .pbo / .bisign / .bikey state of a mod folder.
// It is a structural check (every PBO has a signature whose public key matches
// a shipped bikey); PBO contents are not re-hashed.
type SignatureReport struct {
	Status       string   `json:"status"`
	PboCount     int      `json:"pboCount"`
	SignedCount  int      `json:"signedCount"`
	UnsignedPbos []string `json:"unsignedPbos,omitempty"`
	// UnmatchedPbos are signed PBOs whose signatures match no available bikey.
	UnmatchedPbos  []string `json:"unmatchedPbos,omitempty"`
	UnmatchedCount int      `json:"unmatchedCount,omitempty"`
	Authorities    []string `json:"authorities,omitempty"` // signing authorities seen in bisigns
	Keys           []string `json:"keys,omitempty"`        // authorities of bikeys shipped with the mod
	MissingKeys    []string `json:"missingKeys,omitempty"` // bisign authorities with no matching bikey
}

// KeyID identifies a BI public key by authority name and RSA modulus.
type KeyID struct {
	Authority string
	Modulus   string // hex; empty when the key blob could not be parsed
}

// KeySet is a set of available public keys.
type KeySet map[KeyID]struct{}

// Add inserts a key into the set.
func (s KeySet) Add(k KeyID) { s[k] = struct{}{} }

// Has reports whether k is present. When either side's key blob could not be
// parsed, the authority name alone is compared.
func (s KeySet) Has(k KeyID) bool {
	if _, ok := s[k]; ok {
		return true
	}
	for have := range s {
		if (have.Modulus == "" || k.Modulus == "") && strings.EqualFold(have.Authority, k.Authority) {
			return true
		}
	}
	return false
}

// readKeyHeader parses the common header of .bikey and .bisign files:
// NUL-terminated authority name, uint32 blob length, then a BI/MS PUBLICKEYBLOB
// (BLOBHEADER 8 bytes, "RSA1", bitlen, pubexp, modulus).
func readKeyHeader(path string) (KeyID, bool) {
	f, err := os.Open(path)
	if err != nil {
		return KeyID{}, false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return KeyID{}, false
	}
	buf = buf[:n]
	nul := bytes.IndexByte(buf, 0)
	if nul <= 0 {
		return KeyID{}, false
	}
	id := KeyID{Authority: string(buf[:nul])}
	rest := buf[nul+1:]
	// uint32 length + 20-byte header (BLOBHEADER + RSAPUBKEY) before the modulus.
	if len(rest) < 4+20 {
		return id, true
	}
	blob := rest[4:]
	if string(blob[8:12]) != "RSA1" {
		return id, true
	}
	bitLen := binary.LittleEndian.Uint32(blob[12:16])
	modLen := int(bitLen / 8)
	if modLen <= 0 || 20+modLen > len(blob) {
		return id, true
	}
	id.Modulus = hex.EncodeToString(blob[20 : 20+modLen])
	return id, true
}

// LoadKeySet reads every readable .bikey under dir (recursively).
func LoadKeySet(dir string) KeySet {
	set := KeySet{}
	if dir == "" {
		return set
	}
	files, _ := FindBikeyFiles(dir)
	for _, f := range files {
		if id, ok := readKeyHeader(f); ok {
			set.Add(id)
		}
	}
	return set
}

// findAddonsDir returns modDir/addons (any case), or "".
func findAddonsDir(modDir string) string {
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.EqualFold(e.Name(), "addons") {
			continue
		}
		p := filepath.Join(modDir, e.Name())
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// CheckModSignatures inspects modDir/addons/*.pbo for matching .bisign files and
// checks that each signature's key is available either in the mod's own bikeys
// or in extraKeys (may be nil).
func CheckModSignatures(modDir string, extraKeys KeySet) SignatureReport {
	if modDir == "" {
		return SignatureReport{Status: SignatureUnknown}
	}
	if st, err := os.Stat(modDir); err != nil || !st.IsDir() {
		return SignatureReport{Status: SignatureUnknown}
	}

	report := SignatureReport{}
	modKeys := LoadKeySet(modDir)
	keyNames := map[string]struct{}{}
	for k := range modKeys {
		keyNames[k.Authority] = struct{}{}
	}
	report.Keys = sortedKeys(keyNames)

	addonsDir := findAddonsDir(modDir)
	if addonsDir == "" {
		report.Status = SignatureNoPbos
		return report
	}
	entries, err := os.ReadDir(addonsDir)
	if err != nil {
		report.Status = SignatureUnknown
		return report
	}

	// Group bisigns by lowercased file name for prefix lookup.
	var pbos []string
	var bisigns []string
	for _, e := range entries {
		lower := strings.ToLower(e.Name())
		switch {
		case strings.HasSuffix(lower, ".pbo"):
			// Skip dangling symlinks (stale mirrors) rather than reporting them unsigned.
			if _, err := os.Stat(filepath.Join(addonsDir, e.Name())); err != nil {
				continue
			}
			pbos = append(pbos, e.Name())
		case strings.HasSuffix(lower, ".bisign"):
			bisigns = append(bisigns, e.Name())
		}
	}
	sort.Strings(pbos)
	report.PboCount = len(pbos)
	if len(pbos) == 0 {
		report.Status = SignatureNoPbos
		return report
	}

	sigCache := map[string]KeyID{}
	authorities := map[string]struct{}{}
	missing := map[string]struct{}{}
	var unsigned, unmatched []string

	for _, pbo := range pbos {
		prefix := strings.ToLower(pbo) + "."
		var sigs []KeyID
		for _, sig := range bisigns {
			if !strings.HasPrefix(strings.ToLower(sig), prefix) {
				continue
			}
			id, ok := sigCache[sig]
			if !ok {
				parsed, valid := readKeyHeader(filepath.Join(addonsDir, sig))
				if !valid {
					continue
				}
				id = parsed
				sigCache[sig] = id
			}
			sigs = append(sigs, id)
		}
		if len(sigs) == 0 {
			unsigned = append(unsigned, pbo)
			continue
		}
		report.SignedCount++
		matched := false
		for _, id := range sigs {
			authorities[id.Authority] = struct{}{}
			if modKeys.Has(id) || (extraKeys != nil && extraKeys.Has(id)) {
				matched = true
			}
		}
		if !matched {
			unmatched = append(unmatched, pbo)
			for _, id := range sigs {
				missing[id.Authority] = struct{}{}
			}
		}
	}

	report.Authorities = sortedKeys(authorities)
	report.MissingKeys = sortedKeys(missing)
	report.UnsignedPbos = capList(unsigned)
	report.UnmatchedPbos = capList(unmatched)
	report.UnmatchedCount = len(unmatched)

	switch {
	case len(unsigned) > 0:
		report.Status = SignatureUnsigned
	case len(unmatched) > 0:
		report.Status = SignatureMissingKey
	default:
		report.Status = SignatureOK
	}
	return report
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func capList(s []string) []string {
	if len(s) > maxReportedPbos {
		return s[:maxReportedPbos]
	}
	return s
}
