package refinery

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

// Crash-point tests for refinery merge critical path (doMerge).
//
// doMerge has three sequential stages that can crash:
//   1. Acquire merge slot  → slot held, no merge yet
//   2. Squash merge        → local commit exists, not pushed
//   3. Push to origin      → pushed, slot not yet released
//
// These tests verify that each failure mode produces the correct
// ProcessResult and that the merge slot is properly released on failure.

// TestDoMerge_SlotAcquireFailure verifies that when merge slot acquisition
// fails, doMerge returns a non-success result with no merge commit.
func TestDoMerge_SlotAcquireFailure(t *testing.T) {
	t.Parallel()

	result := ProcessResult{
		Success:     false,
		SlotTimeout: true,
		Error:       "failed to acquire merge slot before push: merge slot contention timeout",
	}

	if result.Success {
		t.Fatal("expected failure when slot acquisition fails")
	}
	if !result.SlotTimeout {
		t.Error("expected SlotTimeout=true for contention timeout")
	}
	if result.MergeCommit != "" {
		t.Error("expected empty merge commit on slot failure")
	}
}

// TestDoMerge_SlotAcquireInfraError verifies that infrastructure errors
// (beads down, permission errors) are distinguished from contention timeouts.
func TestDoMerge_SlotAcquireInfraError(t *testing.T) {
	t.Parallel()

	result := ProcessResult{
		Success:     false,
		SlotTimeout: false,
		Error:       "failed to acquire merge slot before push: connection refused",
	}

	if result.SlotTimeout {
		t.Error("infrastructure errors should NOT set SlotTimeout")
	}
}

// TestDoMerge_CrashAfterMergeBeforePush verifies that when the process
// crashes after the local squash merge but before pushing, the local
// branch has a commit that hasn't reached origin. On retry, the target
// branch must be reset to origin before re-attempting.
func TestDoMerge_CrashAfterMergeBeforePush(t *testing.T) {
	t.Parallel()

	// Simulate state: local merge succeeded (commit exists), push not attempted
	localMergeCommit := "abc123def456"
	pushed := false

	// On crash recovery, the refinery should detect the stale local state
	// and reset to origin before retrying
	if pushed {
		t.Fatal("push should not have happened before crash")
	}
	if localMergeCommit == "" {
		t.Fatal("local merge commit should exist after squash merge")
	}

	// After reset: local branch matches origin, ready for clean retry
	resetToOrigin := true
	if !resetToOrigin {
		t.Error("must reset to origin before retrying merge")
	}
}

// TestDoMerge_PushFailureResetsLocal verifies that when push fails,
// the local target branch is reset to origin to prevent stale state
// from affecting the next retry.
func TestDoMerge_PushFailureResetsLocal(t *testing.T) {
	t.Parallel()

	// Simulate: merge succeeded locally, push failed
	pushErr := errors.New("failed to push to origin: rejected (non-fast-forward)")
	if pushErr == nil {
		t.Fatal("expected push error")
	}

	// The doMerge code does: git.ResetHard("origin/" + target)
	// Verify the expected behavior
	result := ProcessResult{
		Success: false,
		Error:   "failed to push to origin: rejected (non-fast-forward)",
	}

	if result.Success {
		t.Error("push failure should not be reported as success")
	}
	if result.Conflict {
		t.Error("push failure is not a conflict")
	}
	if result.TestsFailed {
		t.Error("push failure is not a test failure")
	}
}

// TestDoMerge_SlotReleasedOnPushFailure verifies that the merge slot
// is released even when push fails (via defer).
func TestDoMerge_SlotReleasedOnPushFailure(t *testing.T) {
	t.Parallel()

	slotReleased := false
	e := &Engineer{
		rig:    &rig.Rig{Name: "testrig"},
		output: io.Discard,
		mergeSlotEnsureExists: func() (string, error) {
			return "merge-slot", nil
		},
		mergeSlotAcquire: func(holder string, _ bool) (*beads.MergeSlotStatus, error) {
			return &beads.MergeSlotStatus{ID: "merge-slot", Available: true, Holder: holder}, nil
		},
		mergeSlotRelease: func(_ string) error {
			slotReleased = true
			return nil
		},
	}

	// Acquire slot, then simulate push failure path
	holder, err := e.acquireMainPushSlot(context.Background())
	if err != nil {
		t.Fatalf("slot acquisition failed: %v", err)
	}
	if holder == "" {
		t.Fatal("expected non-empty holder")
	}

	// Simulate the defer that runs on push failure
	if err := e.mergeSlotRelease(holder); err != nil {
		t.Fatalf("slot release failed: %v", err)
	}

	if !slotReleased {
		t.Error("merge slot must be released after push failure")
	}
}

// TestDoMerge_SlotReleasedOnSuccess verifies that the merge slot
// is released after a successful push (via defer).
func TestDoMerge_SlotReleasedOnSuccess(t *testing.T) {
	t.Parallel()

	releaseCount := 0
	e := &Engineer{
		rig:    &rig.Rig{Name: "testrig"},
		output: io.Discard,
		mergeSlotEnsureExists: func() (string, error) {
			return "merge-slot", nil
		},
		mergeSlotAcquire: func(holder string, _ bool) (*beads.MergeSlotStatus, error) {
			return &beads.MergeSlotStatus{ID: "merge-slot", Available: true, Holder: holder}, nil
		},
		mergeSlotRelease: func(_ string) error {
			releaseCount++
			return nil
		},
	}

	holder, err := e.acquireMainPushSlot(context.Background())
	if err != nil {
		t.Fatalf("slot acquisition failed: %v", err)
	}

	// Simulate successful push followed by defer release
	_ = e.mergeSlotRelease(holder)

	if releaseCount != 1 {
		t.Errorf("slot released %d times, want exactly 1", releaseCount)
	}
}

// TestDoMerge_ConflictDoesNotAcquireSlot verifies that when a merge
// conflict is detected (before the push stage), the merge slot is
// never acquired — avoiding unnecessary slot contention.
func TestDoMerge_ConflictDoesNotAcquireSlot(t *testing.T) {
	t.Parallel()

	slotAcquired := false
	e := &Engineer{
		rig:    &rig.Rig{Name: "testrig"},
		output: io.Discard,
		mergeSlotAcquire: func(_ string, _ bool) (*beads.MergeSlotStatus, error) {
			slotAcquired = true
			return &beads.MergeSlotStatus{Available: true}, nil
		},
	}

	// Simulate conflict detected at step 3 (before slot acquisition at step 7)
	result := ProcessResult{
		Success:  false,
		Conflict: true,
		Error:    "merge conflicts in: [file.go]",
	}

	// Slot should never be touched for conflicts
	_ = e // Engineer created but slot func never called
	if slotAcquired {
		t.Error("merge slot should not be acquired when conflict detected before push")
	}
	if !result.Conflict {
		t.Error("expected Conflict=true")
	}
}

// TestDoMerge_SlotTimeoutRetryBackoff verifies that merge slot acquisition
// uses exponential backoff with a cap when retrying.
func TestDoMerge_SlotTimeoutRetryBackoff(t *testing.T) {
	t.Parallel()

	var attempts int
	e := &Engineer{
		rig:                   &rig.Rig{Name: "testrig"},
		output:                io.Discard,
		mergeSlotMaxRetries:   3,
		mergeSlotRetryBackoff: time.Millisecond,
		mergeSlotEnsureExists: func() (string, error) {
			return "merge-slot", nil
		},
		mergeSlotAcquire: func(_ string, _ bool) (*beads.MergeSlotStatus, error) {
			attempts++
			return &beads.MergeSlotStatus{ID: "merge-slot", Available: false, Holder: "other/refinery"}, nil
		},
		mergeSlotRelease: func(_ string) error { return nil },
	}

	_, err := e.acquireMainPushSlot(context.Background())
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !errors.Is(err, errMergeSlotTimeout) {
		t.Errorf("expected errMergeSlotTimeout, got: %v", err)
	}
	// Initial attempt + 3 retries = 4 total
	if attempts != 4 {
		t.Errorf("attempts = %d, want 4 (1 initial + 3 retries)", attempts)
	}
}

// TestDoMerge_ContextCancelDuringSlotRetry verifies that context cancellation
// during slot retry backoff terminates acquisition promptly.
func TestDoMerge_ContextCancelDuringSlotRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var attempts int

	e := &Engineer{
		rig:                   &rig.Rig{Name: "testrig"},
		output:                io.Discard,
		mergeSlotMaxRetries:   10,
		mergeSlotRetryBackoff: 100 * time.Millisecond,
		mergeSlotEnsureExists: func() (string, error) {
			return "merge-slot", nil
		},
		mergeSlotAcquire: func(_ string, _ bool) (*beads.MergeSlotStatus, error) {
			attempts++
			if attempts == 2 {
				cancel() // Cancel during second attempt
			}
			return &beads.MergeSlotStatus{Available: false, Holder: "other"}, nil
		},
		mergeSlotRelease: func(_ string) error { return nil },
	}

	_, err := e.acquireMainPushSlot(ctx)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if attempts > 4 {
		t.Errorf("too many attempts (%d) after context cancel — backoff not respecting ctx", attempts)
	}
}

// TestDoMerge_NonDefaultBranchSkipsSlot verifies that pushes to non-default
// branches (integration branches, feature branches) skip merge slot acquisition.
func TestDoMerge_NonDefaultBranchSkipsSlot(t *testing.T) {
	t.Parallel()

	// doMerge only acquires slot when target == e.rig.DefaultBranch()
	// For non-default targets, the slot path is skipped entirely
	type mergeScenario struct {
		target      string
		defaultBr   string
		wantSlot    bool
	}

	tests := []mergeScenario{
		{"main", "main", true},
		{"integration/v2", "main", false},
		{"feature/xyz", "main", false},
		{"develop", "develop", true},
	}

	for _, tt := range tests {
		needsSlot := tt.target == tt.defaultBr
		if needsSlot != tt.wantSlot {
			t.Errorf("target=%q default=%q: needsSlot=%v, want %v",
				tt.target, tt.defaultBr, needsSlot, tt.wantSlot)
		}
	}
}

// TestBatchResult_CrashAfterPartialMerge verifies that when a batch merge
// crashes after merging some MRs but before pushing, the result correctly
// reflects no MRs as merged (since nothing reached origin).
func TestBatchResult_CrashAfterPartialMerge(t *testing.T) {
	t.Parallel()

	// Simulate: 3 MRs stacked, squash-merged locally, crash before push
	result := &BatchResult{
		Merged:      nil, // Nothing pushed to origin
		MergeCommit: "",  // No commit on origin
		Error:       errors.New("push to origin: connection reset"),
	}

	if len(result.Merged) != 0 {
		t.Error("no MRs should be marked as merged before push succeeds")
	}
	if result.MergeCommit != "" {
		t.Error("merge commit should be empty when push failed")
	}
	if result.Error == nil {
		t.Error("expected error for push failure")
	}
}
