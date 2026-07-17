package server

import "testing"

func TestLatestGenericBox64Deb(t *testing.T) {
	index := `
Package: box64-rpi5arm64
Version: 9.9.9
Filename: ./box64-rpi5arm64_9.9.9_arm64.deb
SHA256: aaa

Package: box64
Version: 0.4.3+old
Filename: ./box64_old_arm64.deb
SHA256: bbb

Package: box64
Version: 0.4.3+new
Filename: ./box64_new_arm64.deb
SHA256: ccc
Size: 100
`
	info, err := latestGenericBox64Deb(index)
	if err != nil {
		t.Fatal(err)
	}
	if info.Filename != "box64_new_arm64.deb" {
		t.Fatalf("filename = %q", info.Filename)
	}
	if info.Version != "0.4.3+new" {
		t.Fatalf("version = %q", info.Version)
	}
	if info.SHA256 != "ccc" {
		t.Fatalf("sha = %q", info.SHA256)
	}
}

func TestLatestGenericBox64DebMissing(t *testing.T) {
	_, err := latestGenericBox64Deb("Package: box64-rpi4arm64\nFilename: ./x.deb\n")
	if err == nil {
		t.Fatal("expected error")
	}
}
