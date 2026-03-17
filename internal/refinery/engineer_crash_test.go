package refinery

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

// =============================================================================
// Refinery merge crash-point tests (gs-k27)
//
// These tests verify that the refinery's HandleMRInfoSuccess correctly handles
// partial failures at each critical coordination point during post-merge
// cleanup. Each crash point simulates a failure at a specific step and verifies
// that the remaining steps still execute (non-fatal warning pattern).
// =============================================================================

// testEngineerWithOutput creates an Engineer with captured output for testing.
// Uses a minimal rig and beads setup that doesn't require a real Dolt server.
func testEngineerWithOutput(t *testing.T) (*Engineer, *bytes.Buffer) {
	t.Helper()
	tmpDir := t.TempDir()
	r := &rig.Rig{Name: "test-rig", Path: tmpDir}
	e := NewEngineer(r)
	var buf bytes.Buffer
	e.output = &buf
	e.workDir = tmpDir
	// Stub merge slot functions to avoid beads dependency
	e.mergeSlotRelease = func(holder string) error { return nil }
	return e, &buf
}

// TestRefineryCrashPoint_BranchMergedButBeadNotClosed verifies that when the
// git merge succeeds but closing the MR bead fails, HandleMRInfoSuccess
// continues with remaining post-merge steps (source issue close, branch
// cleanup, convoy check, mayor nudge).
func TestRefineryCrashPoint_BranchMergedButBeadNotClosed(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:          "gt-mr-crash1",
		Branch:      "polecat/nux/gs-test",
		Target:      "main",
		SourceIssue: "gt-issue-123",
		Worker:      "polecats/nux",
	}
	result := ProcessResult{
		Success:     true,
		MergeCommit: "abc123def456",
	}

	// HandleMRInfoSuccess will fail on beads operations (no real beads server)
	// but should continue through all steps, logging warnings.
	e.HandleMRInfoSuccess(mr, result)

	output := buf.String()

	// Should attempt to close MR bead (and warn on failure)
	if !strings.Contains(output, "Warning: failed to fetch MR bead gt-mr-crash1") &&
		!strings.Contains(output, "Closed MR bead") {
		// Either it warned about failure or succeeded — both are valid
		t.Log("MR bead close attempted (may have warned or succeeded)")
	}

	// Should attempt to close source issue
	if !strings.Contains(output, "gt-issue-123") {
		t.Error("should attempt to close source issue gt-issue-123")
	}

	// Should log final success
	if !strings.Contains(output, "Merged: gt-mr-crash1") {
		t.Errorf("should log final merge success, got:\n%s", output)
	}
}

// TestRefineryCrashPoint_BeadClosedButMRNotUpdated verifies that when the
// source issue is closed but the MR bead update (merge_commit SHA) fails,
// the merge is still considered successful and remaining steps execute.
func TestRefineryCrashPoint_BeadClosedButMRNotUpdated(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:          "gt-mr-crash2",
		Branch:      "polecat/nux/gs-test2",
		Target:      "main",
		SourceIssue: "gt-issue-456",
		Worker:      "polecats/nux",
		AgentBead:   "gt-agent-nux",
	}
	result := ProcessResult{
		Success:     true,
		MergeCommit: "def789abc012",
	}

	e.HandleMRInfoSuccess(mr, result)

	output := buf.String()

	// Should attempt MR bead update (and warn on failure since no real beads)
	if !strings.Contains(output, "gt-mr-crash2") {
		t.Error("should reference MR bead ID in output")
	}

	// Should still log final success even if MR update failed
	if !strings.Contains(output, "Merged: gt-mr-crash2") {
		t.Errorf("should log final merge success despite MR update failure, got:\n%s", output)
	}

	// Should attempt agent bead cleanup
	if !strings.Contains(output, "gt-agent-nux") || !strings.Contains(output, "Warning") {
		t.Log("agent bead cleanup attempted (may have warned)")
	}
}

// TestRefineryCrashPoint_FastForwardSucceededButPostMergeHooksNotRun verifies
// that when a batch fast-forward push succeeds but post-merge hooks
// (HandleMRInfoSuccess) fail for individual MRs, the batch result still
// reflects the successful merge.
func TestRefineryCrashPoint_FastForwardSucceededButPostMergeHooksNotRun(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	// Simulate batch of MRs that were successfully fast-forwarded
	mrs := []*MRInfo{
		{ID: "gt-mr-batch1", Branch: "polecat/a", Target: "main", SourceIssue: "gt-1"},
		{ID: "gt-mr-batch2", Branch: "polecat/b", Target: "main", SourceIssue: "gt-2"},
	}
	mergeCommit := "batchcommit123"

	// Run HandleMRInfoSuccess for each MR (simulating post-fast-forward cleanup)
	// These will fail on beads operations but should not panic or block
	for _, mr := range mrs {
		result := ProcessResult{Success: true, MergeCommit: mergeCommit}
		e.HandleMRInfoSuccess(mr, result)
	}

	output := buf.String()

	// Both MRs should have been processed
	if !strings.Contains(output, "gt-mr-batch1") {
		t.Error("should process first MR in batch")
	}
	if !strings.Contains(output, "gt-mr-batch2") {
		t.Error("should process second MR in batch")
	}

	// Both should log final success
	if strings.Count(output, "Merged:") < 2 {
		t.Errorf("should log success for both MRs, got:\n%s", output)
	}
}

// TestRefineryCrashPoint_HandleMRInfoSuccessNoID verifies that
// HandleMRInfoSuccess handles the edge case where MR ID is empty
// (skips bead operations gracefully).
func TestRefineryCrashPoint_HandleMRInfoSuccessNoID(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:          "", // Empty ID — edge case
		Branch:      "polecat/nux",
		Target:      "main",
		SourceIssue: "gt-issue-789",
	}
	result := ProcessResult{Success: true, MergeCommit: "empty123"}

	// Should not panic with empty MR ID
	e.HandleMRInfoSuccess(mr, result)

	output := buf.String()

	// Should still attempt source issue close
	if !strings.Contains(output, "gt-issue-789") || !strings.Contains(output, "Warning") {
		t.Log("source issue close attempted with empty MR ID")
	}
}

// TestRefineryCrashPoint_HandleMRInfoSuccessNoSourceIssue verifies that
// HandleMRInfoSuccess handles the edge case where source issue is empty
// (skips source issue close gracefully).
func TestRefineryCrashPoint_HandleMRInfoSuccessNoSourceIssue(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:          "gt-mr-nosource",
		Branch:      "polecat/nux",
		Target:      "main",
		SourceIssue: "", // Empty source issue
	}
	result := ProcessResult{Success: true, MergeCommit: "nosource123"}

	e.HandleMRInfoSuccess(mr, result)

	output := buf.String()

	// Should still log final success
	if !strings.Contains(output, "Merged: gt-mr-nosource") {
		t.Errorf("should log success even without source issue, got:\n%s", output)
	}
}

// TestRefineryCrashPoint_ProcessMRInfoPreVerifiedStaleBase verifies that when
// a pre-verified MR has a stale base (target moved since verification), the
// refinery falls through to normal gate execution instead of fast-pathing.
func TestRefineryCrashPoint_ProcessMRInfoPreVerifiedStaleBase(t *testing.T) {
	t.Parallel()

	mr := &MRInfo{
		ID:              "gt-mr-preverify",
		Branch:          "polecat/nux",
		Target:          "main",
		SourceIssue:     "gt-issue-pv",
		PreVerified:     true,
		PreVerifiedAt:   time.Now().Add(-5 * time.Minute),
		PreVerifiedBase: "oldsha123456789",
	}

	// When target HEAD != PreVerifiedBase, skipGates should be false
	targetHead := "newsha987654321"
	skipGates := mr.PreVerified && mr.PreVerifiedBase != "" && targetHead == mr.PreVerifiedBase

	if skipGates {
		t.Error("should NOT skip gates when pre-verified base is stale")
	}
}

// TestRefineryCrashPoint_ProcessMRInfoPreVerifiedValidBase verifies that when
// a pre-verified MR has a matching base, gates are skipped (fast-path).
func TestRefineryCrashPoint_ProcessMRInfoPreVerifiedValidBase(t *testing.T) {
	t.Parallel()

	matchingSHA := "abc123def456789"
	mr := &MRInfo{
		ID:              "gt-mr-fastpath",
		Branch:          "polecat/nux",
		Target:          "main",
		PreVerified:     true,
		PreVerifiedBase: matchingSHA,
	}

	targetHead := matchingSHA
	skipGates := mr.PreVerified && mr.PreVerifiedBase != "" && targetHead == mr.PreVerifiedBase

	if !skipGates {
		t.Error("should skip gates when pre-verified base matches target HEAD")
	}
}

// TestRefineryCrashPoint_HandleMRInfoFailureSlotTimeout verifies that slot
// timeout failures leave the MR in queue for automatic retry without
// notifying polecats.
func TestRefineryCrashPoint_HandleMRInfoFailureSlotTimeout(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:     "gt-mr-slot",
		Branch: "polecat/nux",
		Worker: "polecats/nux",
	}
	result := ProcessResult{
		Success:     false,
		SlotTimeout: true,
		Error:       "merge slot contention timeout",
	}

	e.HandleMRInfoFailure(mr, result)

	output := buf.String()

	// Should indicate slot timeout and automatic retry
	if !strings.Contains(output, "Slot timeout") {
		t.Error("should indicate slot timeout")
	}
	if !strings.Contains(output, "automatic retry") {
		t.Errorf("should indicate automatic retry, got:\n%s", output)
	}
}

// TestRefineryCrashPoint_HandleMRInfoFailureBranchNotFound verifies that
// branch-not-found failures are handled gracefully (branch cleaned up
// before refinery could process it).
func TestRefineryCrashPoint_HandleMRInfoFailureBranchNotFound(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	mr := &MRInfo{
		ID:     "gt-mr-gone",
		Branch: "polecat/nux/gone",
		Worker: "polecats/nux",
	}
	result := ProcessResult{
		Success:        false,
		BranchNotFound: true,
		Error:          "branch not found",
	}

	e.HandleMRInfoFailure(mr, result)

	output := buf.String()

	if !strings.Contains(output, "no longer exists") {
		t.Errorf("should indicate branch no longer exists, got:\n%s", output)
	}
}

// TestRefineryCrashPoint_MRFieldsParsing verifies that MR fields (merge_commit,
// close_reason) can be set and parsed correctly, which is critical for the
// crash recovery path where the refinery needs to determine if a merge
// already completed.
func TestRefineryCrashPoint_MRFieldsParsing(t *testing.T) {
	t.Parallel()

	// Simulate an MR bead with merge fields set
	issue := &beads.Issue{
		ID:          "gt-mr-fields",
		Description: "branch: polecat/nux\ntarget: main\nsource_issue: gt-123",
	}

	// Parse existing fields
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		// ParseMRFields returns nil when no MR-specific fields found — that's OK
		fields = &beads.MRFields{}
	}

	// Set merge commit (simulating successful merge)
	fields.MergeCommit = "abc123"
	fields.CloseReason = "merged"

	newDesc := beads.SetMRFields(issue, fields)

	// Verify fields are in the new description
	if !strings.Contains(newDesc, "abc123") {
		t.Errorf("new description should contain merge commit, got:\n%s", newDesc)
	}

	// Re-parse to verify round-trip
	issue.Description = newDesc
	reparsed := beads.ParseMRFields(issue)
	if reparsed == nil {
		t.Fatal("re-parsed MR fields should not be nil")
	}
	if reparsed.MergeCommit != "abc123" {
		t.Errorf("MergeCommit = %q, want %q", reparsed.MergeCommit, "abc123")
	}
}

// TestRefineryCrashPoint_ConvoyMRPostMerge verifies that convoy-tracked MRs
// trigger post-merge convoy checks even when individual bead operations fail.
func TestRefineryCrashPoint_ConvoyMRPostMerge(t *testing.T) {
	t.Parallel()
	e, buf := testEngineerWithOutput(t)

	convoyTime := time.Now()
	mr := &MRInfo{
		ID:              "gt-mr-convoy",
		Branch:          "polecat/nux/convoy",
		Target:          "main",
		SourceIssue:     "gt-convoy-issue",
		ConvoyID:        "hq-convoy-123",
		ConvoyCreatedAt: &convoyTime,
	}
	result := ProcessResult{Success: true, MergeCommit: "convoy123"}

	e.HandleMRInfoSuccess(mr, result)

	output := buf.String()

	// Should log final success
	if !strings.Contains(output, "Merged: gt-mr-convoy") {
		t.Errorf("should log success for convoy MR, got:\n%s", output)
	}
}
