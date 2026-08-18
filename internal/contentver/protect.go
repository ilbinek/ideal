package contentver

import (
	"fmt"
	"sync"
)

// In-memory protection for generations that are currently being written.
// They are recorded as not-current and unpinned, so orphan GC would otherwise
// delete them if a server on the same branch/mod stops mid-install.
//
// Process-local on purpose: a crash should still let GCAllOrphans clean leftovers.
var (
	protectMu     sync.Mutex
	pendingBranch = map[string]struct{}{}
	pendingMod    = map[string]struct{}{}
)

func genKey(id, generation int) string {
	return fmt.Sprintf("%d:%d", id, generation)
}

// ProtectBranchGeneration keeps a non-current branch generation from being GC'd
// until UnprotectBranchGeneration (Finalize/Abort).
func ProtectBranchGeneration(branchID, generation int) {
	protectMu.Lock()
	defer protectMu.Unlock()
	pendingBranch[genKey(branchID, generation)] = struct{}{}
}

// UnprotectBranchGeneration allows orphan GC to consider the generation again.
func UnprotectBranchGeneration(branchID, generation int) {
	protectMu.Lock()
	defer protectMu.Unlock()
	delete(pendingBranch, genKey(branchID, generation))
}

func branchGenerationProtected(branchID, generation int) bool {
	protectMu.Lock()
	defer protectMu.Unlock()
	_, ok := pendingBranch[genKey(branchID, generation)]
	return ok
}

// ProtectModGeneration keeps a non-current mod generation from being GC'd
// until UnprotectModGeneration (Finalize/Abort).
func ProtectModGeneration(modID, generation int) {
	protectMu.Lock()
	defer protectMu.Unlock()
	pendingMod[genKey(modID, generation)] = struct{}{}
}

// UnprotectModGeneration allows orphan GC to consider the generation again.
func UnprotectModGeneration(modID, generation int) {
	protectMu.Lock()
	defer protectMu.Unlock()
	delete(pendingMod, genKey(modID, generation))
}

func modGenerationProtected(modID, generation int) bool {
	protectMu.Lock()
	defer protectMu.Unlock()
	_, ok := pendingMod[genKey(modID, generation)]
	return ok
}
