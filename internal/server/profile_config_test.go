package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerProfileLaunchArgs(t *testing.T) {
	tests := []struct {
		input       string
		wantProfiles string
		wantName    string
	}{
		{"tbd_public", "profiles", "tbd_public"},
		{"Server", "profiles", "Server"},
		{"", "profiles", "Server"},
		{"  ", "profiles", "Server"},
	}

	for _, tt := range tests {
		profiles, name := serverProfileLaunchArgs(tt.input)
		if profiles != tt.wantProfiles {
			t.Errorf("serverProfileLaunchArgs(%q) profiles = %q, want %q", tt.input, profiles, tt.wantProfiles)
		}
		if name != tt.wantName {
			t.Errorf("serverProfileLaunchArgs(%q) name = %q, want %q", tt.input, name, tt.wantName)
		}
	}
}

func TestCanonicalProfileDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", dir)

	got := canonicalProfileDir("my-server", "tbd_public")
	want := filepath.Join(dir, "servers", "my-server", "profiles", "Users", "tbd_public")
	if got != want {
		t.Fatalf("canonicalProfileDir() = %q, want %q", got, want)
	}

	gotDefault := canonicalProfileDir("my-server", "")
	wantDefault := filepath.Join(dir, "servers", "my-server", "profiles", "Users", "Server")
	if gotDefault != wantDefault {
		t.Fatalf("canonicalProfileDir() with empty name = %q, want %q", gotDefault, wantDefault)
	}
}

func TestResolveProfileDirPrefersCanonical(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", dir)
	slug := "test-server"
	name := "tbd_public"

	canonical := canonicalProfileDir(slug, name)
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "tbd_public.Arma3Profile"), []byte("class"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveProfileDir(slug, name)
	if got != canonical {
		t.Fatalf("resolveProfileDir() = %q, want canonical %q", got, canonical)
	}
}

func TestResolveProfileDirLegacyNestedPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", dir)
	slug := "test-server"
	name := "tbd_public"

	legacy := filepath.Join(InstanceDir(slug), "profiles", name, "Users", name)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "tbd_public.Arma3Profile"), []byte("class"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveProfileDir(slug, name)
	if got != legacy {
		t.Fatalf("resolveProfileDir() = %q, want legacy %q", got, legacy)
	}
}

func TestResolveProfileDirLegacyFlatPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", dir)
	slug := "test-server"
	name := "Server"

	legacy := filepath.Join(InstanceDir(slug), "profiles", name)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "Server.Arma3Profile"), []byte("class"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveProfileDir(slug, name)
	if got != legacy {
		t.Fatalf("resolveProfileDir() = %q, want legacy %q", got, legacy)
	}
}

func TestListProfileFilesFromResolvedDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", dir)
	slug := "test-server"
	name := "tbd_public"

	profilePath := canonicalProfileDir(slug, name)
	if err := os.MkdirAll(profilePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilePath, "tbd_public.Arma3Profile"), []byte("class"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilePath, "Arma3.cfg"), []byte("language"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := ListProfileFiles(slug, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("ListProfileFiles() returned %d files, want 2", len(files))
	}
}
