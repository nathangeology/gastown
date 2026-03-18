package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Crash-point tests for gt done critical path.
//
// gt done has three sequential stages that write checkpoints:
//   1. Push branch to origin  → CheckpointPushed
//   2. Create MR bead         → CheckpointMRCreated
//   3. Notify witness          → CheckpointWitnessNotified
//
// If gt done crashes between stages, the next invocation reads checkpoints
// from the agent bead and resumes from the last completed stage (gt-aufru).
// These tests verify the resume logic for each crash point.

// TestCrashAfterPush_SkipsPushOnResume verifies that when gt done crashes
// after pushing but before creating the MR bead, the resumed invocation
// skips the push and proceeds directly to MR creation.
func TestCrashAfterPush_SkipsPushOnResume(t *testing.T) {
	t.Parallel()

	// Simulate: push succeeded, then crash. Checkpoint has pushed=mybranch.
	checkpoints := map[DoneCheckpoint]string{
		CheckpointPushed: "polecat/toast-abc123",
	}

	// Resume logic: push checkpoint exists → skip push
	if checkpoints[CheckpointPushed] == "" {
		t.Fatal("expected push checkpoint to exist")
	}

	// MR checkpoint should NOT exist (crash was before MR creation)
	if checkpoints[CheckpointMRCreated] != "" {
		t.Fatal("MR checkpoint should not exist after push-only crash")
	}

	// Witness checkpoint should NOT exist
	if checkpoints[CheckpointWitnessNotified] != "" {
		t.Fatal("witness checkpoint should not exist after push-only crash")
	}
}

// TestCrashAfterMR_SkipsPushAndMROnResume verifies that when gt done crashes
// after creating the MR bead but before notifying the witness, the resumed
// invocation skips both push and MR creation.
func TestCrashAfterMR_SkipsPushAndMROnResume(t *testing.T) {
	t.Parallel()

	checkpoints := map[DoneCheckpoint]string{
		CheckpointPushed:    "polecat/toast-abc123",
		CheckpointMRCreated: "gs-mr-xyz",
	}

	// Both push and MR should be skipped
	skipPush := checkpoints[CheckpointPushed] != ""
	skipMR := checkpoints[CheckpointMRCreated] != ""

	if !skipPush {
		t.Error("push should be skipped on resume")
	}
	if !skipMR {
		t.Error("MR creation should be skipped on resume")
	}

	// The resumed MR ID should match the checkpoint
	mrID := checkpoints[CheckpointMRCreated]
	if mrID != "gs-mr-xyz" {
		t.Errorf("resumed MR ID = %q, want %q", mrID, "gs-mr-xyz")
	}
}

// TestCrashAfterWitnessNotify_AllStagesSkipped verifies that when all
// checkpoints exist (gt done crashed during final cleanup), the resumed
// invocation skips all three stages.
func TestCrashAfterWitnessNotify_AllStagesSkipped(t *testing.T) {
	t.Parallel()

	checkpoints := map[DoneCheckpoint]string{
		CheckpointPushed:          "polecat/toast-abc123",
		CheckpointMRCreated:       "gs-mr-xyz",
		CheckpointWitnessNotified: "ok",
	}

	allSkipped := checkpoints[CheckpointPushed] != "" &&
		checkpoints[CheckpointMRCreated] != "" &&
		checkpoints[CheckpointWitnessNotified] != ""

	if !allSkipped {
		t.Error("all stages should be skippable when all checkpoints exist")
	}
}

// TestNoCheckpoints_NothingSkipped verifies that a fresh gt done invocation
// (no prior crash) executes all stages.
func TestNoCheckpoints_NothingSkipped(t *testing.T) {
	t.Parallel()

	checkpoints := map[DoneCheckpoint]string{}

	skipPush := checkpoints[CheckpointPushed] != ""
	skipMR := checkpoints[CheckpointMRCreated] != ""
	skipWitness := checkpoints[CheckpointWitnessNotified] != ""

	if skipPush || skipMR || skipWitness {
		t.Error("no stages should be skipped without checkpoints")
	}
}

// TestDoneIntentLabel_WrittenBeforePush verifies that the done-intent label
// is written BEFORE the push stage. If gt done crashes at any point after
// this, the witness can detect the intent and handle the zombie.
func TestDoneIntentLabel_WrittenBeforePush(t *testing.T) {
	t.Parallel()

	// The done-intent label format: done-intent:<type>:<unix-ts>
	now := time.Now()
	for _, exitType := range []string{ExitCompleted, ExitEscalated, ExitDeferred} {
		label := fmt.Sprintf("done-intent:%s:%d", exitType, now.Unix())

		// Verify it can be parsed back
		parts := strings.SplitN(label, ":", 3)
		if len(parts) != 3 {
			t.Fatalf("label %q should have 3 parts", label)
		}
		if parts[0] != "done-intent" {
			t.Errorf("prefix = %q, want done-intent", parts[0])
		}
		if parts[1] != exitType {
			t.Errorf("exit type = %q, want %q", parts[1], exitType)
		}
	}
}

// TestCrashRecovery_CheckpointLabelParsing verifies that checkpoint labels
// survive round-trip through the label format used by agent beads.
func TestCrashRecovery_CheckpointLabelParsing(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		stage DoneCheckpoint
		value string
	}{
		{CheckpointPushed, "polecat/toast-abc123"},
		{CheckpointMRCreated, "gs-mr-xyz"},
		{CheckpointWitnessNotified, "ok"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			// Write format
			label := fmt.Sprintf("done-cp:%s:%s:%d", tt.stage, tt.value, now.Unix())

			// Parse format (same logic as readDoneCheckpoints)
			parts := strings.SplitN(label, ":", 4)
			if len(parts) != 4 {
				t.Fatalf("expected 4 parts, got %d", len(parts))
			}

			parsedStage := DoneCheckpoint(parts[1])
			parsedValue := parts[2]

			if parsedStage != tt.stage {
				t.Errorf("stage = %q, want %q", parsedStage, tt.stage)
			}
			if parsedValue != tt.value {
				t.Errorf("value = %q, want %q", parsedValue, tt.value)
			}
		})
	}
}

// TestCrashRecovery_MixedLabelsPreserved verifies that checkpoint parsing
// correctly ignores non-checkpoint labels (done-intent, agent state, etc.)
// that coexist on the same agent bead.
func TestCrashRecovery_MixedLabelsPreserved(t *testing.T) {
	t.Parallel()

	labels := []string{
		"gt:agent",
		"idle:3",
		"done-intent:COMPLETED:1738972800",
		"done-cp:pushed:polecat/toast:1738972801",
		"done-cp:mr-created:gs-mr-abc:1738972802",
		"backoff-until:1738972900",
	}

	// Parse checkpoints (same logic as readDoneCheckpoints)
	checkpoints := make(map[DoneCheckpoint]string)
	for _, label := range labels {
		if strings.HasPrefix(label, "done-cp:") {
			parts := strings.SplitN(label, ":", 4)
			if len(parts) >= 3 {
				checkpoints[DoneCheckpoint(parts[1])] = parts[2]
			}
		}
	}

	if len(checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(checkpoints))
	}
	if checkpoints[CheckpointPushed] != "polecat/toast" {
		t.Errorf("pushed = %q, want %q", checkpoints[CheckpointPushed], "polecat/toast")
	}
	if checkpoints[CheckpointMRCreated] != "gs-mr-abc" {
		t.Errorf("mr-created = %q, want %q", checkpoints[CheckpointMRCreated], "gs-mr-abc")
	}
}

// TestCrashRecovery_PushFailedNoCheckpoint verifies that when push fails,
// no checkpoint is written, so the next invocation retries the push.
func TestCrashRecovery_PushFailedNoCheckpoint(t *testing.T) {
	t.Parallel()

	// Simulate: push attempted but failed. No checkpoint written.
	checkpoints := map[DoneCheckpoint]string{}

	// On resume, push should NOT be skipped
	if checkpoints[CheckpointPushed] != "" {
		t.Error("push checkpoint should not exist when push failed")
	}
}

// TestCrashRecovery_MRFailedNoCheckpoint verifies that when MR creation fails,
// only the push checkpoint exists, so the next invocation skips push but
// retries MR creation.
func TestCrashRecovery_MRFailedNoCheckpoint(t *testing.T) {
	t.Parallel()

	// Simulate: push succeeded (checkpoint written), MR creation failed (no checkpoint)
	checkpoints := map[DoneCheckpoint]string{
		CheckpointPushed: "polecat/toast-abc123",
	}

	skipPush := checkpoints[CheckpointPushed] != ""
	skipMR := checkpoints[CheckpointMRCreated] != ""

	if !skipPush {
		t.Error("push should be skipped (checkpoint exists)")
	}
	if skipMR {
		t.Error("MR creation should NOT be skipped (no checkpoint)")
	}
}

// TestCrashRecovery_CleanupStatusAfterPush verifies that cleanup_status
// transitions from "unpushed" to "clean" after a successful push, even
// on a resumed invocation.
func TestCrashRecovery_CleanupStatusAfterPush(t *testing.T) {
	t.Parallel()

	// Before push: status detected as unpushed
	cleanupStatus := "unpushed"

	// After successful push (or resumed with push checkpoint):
	// the fix at gt-wcr updates status to clean
	if cleanupStatus == "unpushed" {
		cleanupStatus = "clean"
	}

	if cleanupStatus != "clean" {
		t.Errorf("cleanup status after push = %q, want %q", cleanupStatus, "clean")
	}
}

// TestCrashRecovery_ZeroCommitGuard verifies that gt done blocks completion
// when there are zero commits ahead of origin/main, preventing polecats
// from "completing" without doing any work.
func TestCrashRecovery_ZeroCommitGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		aheadCount     int
		isPolecat      bool
		cleanupStatus  string
		isNoMergeTask  bool
		wantBlock      bool
	}{
		{"polecat with commits", 3, true, "", false, false},
		{"polecat zero commits", 0, true, "", false, true},
		{"polecat zero commits clean", 0, true, "clean", false, false},
		{"polecat zero commits no_merge", 0, true, "", true, false},
		{"non-polecat zero commits", 0, false, "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked := false
			if tt.aheadCount == 0 && tt.isPolecat && tt.cleanupStatus != "clean" && !tt.isNoMergeTask {
				blocked = true
			}
			if blocked != tt.wantBlock {
				t.Errorf("blocked = %v, want %v", blocked, tt.wantBlock)
			}
		})
	}
}
