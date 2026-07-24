//go:build unix

package server

import (
	"os"
	"path/filepath"
	"testing"

	"arma3modmanager/internal/utils"
)

func TestListLogsSkipsUnreadableProfiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", root)

	slug := "antistasi"
	logsDir := filepath.Join(root, "servers", slug, "logs")
	profilesDir := filepath.Join(root, "servers", slug, "profiles", "locked")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(logsDir, "session-2026-07-24_050558.log")
	if err := os.WriteFile(session, []byte("hello from arma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "secret.rpt"), []byte("hidden\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(profilesDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(profilesDir, 0755) })

	if got := utils.DataRoot(); got != root {
		t.Fatalf("DataRoot=%q want %q", got, root)
	}

	entries, err := ListLogs(slug)
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected session log to be listed despite unreadable profiles/")
	}
	found := false
	for _, e := range entries {
		if e.Name == "session-2026-07-24_050558.log" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session log missing from entries: %+v", entries)
	}
}
