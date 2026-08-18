package contentver

import (
	"os"
	"path/filepath"
	"testing"

	"arma3modmanager/internal/database"
)

func TestGCSkipsProtectedBranchGeneration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("IDEAL_DATA_DIR", tmp)
	if err := database.Init(); err != nil {
		t.Fatalf("database.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	installPath := filepath.Join(tmp, "arma3", "branches", "gctest")
	id64, err := database.CreateBranch("GCTest", "", installPath)
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	branchID := int(id64)

	gen1 := BranchGenerationDir(installPath, 1)
	gen2 := BranchGenerationDir(installPath, 2)
	if err := os.MkdirAll(gen1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gen2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertBranchGeneration(branchID, 1, "1", "", true); err != nil {
		t.Fatalf("upsert gen 1: %v", err)
	}
	if err := database.UpsertBranchGeneration(branchID, 2, "2", "", false); err != nil {
		t.Fatalf("upsert gen 2: %v", err)
	}

	ProtectBranchGeneration(branchID, 2)
	t.Cleanup(func() { UnprotectBranchGeneration(branchID, 2) })

	GCBranchOrphans(branchID, installPath)
	if _, err := os.Stat(gen2); err != nil {
		t.Fatalf("protected gen 2 was collected: %v", err)
	}
	if _, err := os.Stat(gen1); err != nil {
		t.Fatalf("current gen 1 was collected: %v", err)
	}

	GCAllOrphans()
	if _, err := os.Stat(gen2); err != nil {
		t.Fatalf("protected gen 2 was collected by GCAllOrphans: %v", err)
	}

	UnprotectBranchGeneration(branchID, 2)
	GCBranchOrphans(branchID, installPath)
	if _, err := os.Stat(gen2); !os.IsNotExist(err) {
		t.Fatal("unprotected gen 2 should have been collected")
	}
	if _, err := os.Stat(gen1); err != nil {
		t.Fatalf("current gen 1 should remain: %v", err)
	}
}
