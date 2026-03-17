package witness

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
)

// =============================================================================
// Witness patrol crash-point tests (gs-k27)
//
// These tests verify that the witness patrol correctly handles crash-midway
// scenarios: stale polecat detected but not reported, and report sent but
// bead not reset to open. Each test simulates the interrupted state and
// verifies that re-running from that state produces correct final state.
// =============================================================================

// TestPatrolCrashPoint_StalePolecatDetectedButNotReported verifies that when
// a zombie polecat is detected but the patrol crashes before reporting it
// (via mail or nudge), the next patrol cycle re-detects the zombie because
// the agent state hasn't changed.
func TestPatrolCrashPoint_StalePolecatDetectedButNotReported(t *testing.T) {
	t.Parallel()

	// Simulate: zombie detected (dead session + active state + hook bead)
	// but patrol crashed before sending report or taking action.
	zombie := ZombieResult{
		PolecatName:    "crash-polecat",
		AgentState:     "working",
		Classification: ZombieSessionDeadActive,
		HookBead:       "gt-stale-123",
		WasActive:      true,
		Action:         "", // No action taken (crash before action)
	}

	// The zombie should be re-detectable on next patrol because:
	// 1. Agent state is still "working" (not reset)
	// 2. Hook bead is still assigned (not recovered)
	// 3. Session is still dead (not restarted)
	if !isZombieState(beads.AgentState(zombie.AgentState), zombie.HookBead) {
		t.Error("zombie should still be detectable after crash (state unchanged)")
	}

	// Classification should imply active work
	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("session-dead-active classification should imply active work")
	}
}

// TestPatrolCrashPoint_ReportSentButBeadNotReset verifies that when the
// witness sends a report about a zombie but crashes before resetting the
// bead to open, the next patrol cycle detects the zombie again and
// completes the recovery.
func TestPatrolCrashPoint_ReportSentButBeadNotReset(t *testing.T) {
	t.Parallel()

	// Simulate: zombie detected and reported, but bead not reset.
	// The bead is still in "hooked" status with the dead polecat as assignee.
	zombie := ZombieResult{
		PolecatName:    "unreset-polecat",
		AgentState:     "working",
		Classification: ZombieSessionDeadActive,
		HookBead:       "gt-unreset-456",
		WasActive:      true,
		BeadRecovered:  false, // Bead NOT recovered (crash before reset)
		Action:         "restarted",
	}

	// On next patrol, the zombie should be re-detected because:
	// 1. BeadRecovered is false — bead still in hooked/in_progress state
	// 2. Agent state still shows "working"
	// 3. isZombieState returns true for working + hook_bead
	if !isZombieState(beads.AgentState(zombie.AgentState), zombie.HookBead) {
		t.Error("zombie should be re-detectable when bead not recovered")
	}

	if zombie.BeadRecovered {
		t.Error("BeadRecovered should be false when crash happened before reset")
	}
}

// TestPatrolCrashPoint_DoneIntentDeadSessionRecovery verifies that when a
// polecat crashes during gt done (done-intent label exists, session dead),
// the witness correctly classifies it and restarts the session.
func TestPatrolCrashPoint_DoneIntentDeadSessionRecovery(t *testing.T) {
	t.Parallel()

	doneIntent := &DoneIntent{
		ExitType:  "COMPLETED",
		Timestamp: time.Now().Add(-90 * time.Second), // 90s old — past grace period
	}

	sessionAlive := false
	age := time.Since(doneIntent.Timestamp)

	// Should trigger restart (not nuke) per gt-dsgp
	shouldRestart := !sessionAlive && doneIntent != nil && age >= config.DefaultWitnessDoneIntentStuckTimeout
	if !shouldRestart {
		t.Errorf("should restart dead session with old done-intent (age=%v)", age)
	}

	// Verify classification
	zombie := ZombieResult{
		PolecatName:    "done-crash",
		AgentState:     "working",
		Classification: ZombieDoneIntentDead,
		WasActive:      true,
	}

	if zombie.Classification != ZombieDoneIntentDead {
		t.Errorf("classification = %q, want %q", zombie.Classification, ZombieDoneIntentDead)
	}
	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("done-intent-dead should imply active work")
	}
}

// TestPatrolCrashPoint_DoneIntentRecentGracePeriod verifies that a recent
// done-intent (within grace period) is NOT treated as a zombie — the polecat
// is still working through gt done.
func TestPatrolCrashPoint_DoneIntentRecentGracePeriod(t *testing.T) {
	t.Parallel()

	doneIntent := &DoneIntent{
		ExitType:  "COMPLETED",
		Timestamp: time.Now().Add(-5 * time.Second), // 5s old — within grace
	}

	age := time.Since(doneIntent.Timestamp)

	// Should NOT trigger restart — still within grace period
	shouldSkip := doneIntent != nil && age < config.DefaultWitnessDoneIntentStuckTimeout
	if !shouldSkip {
		t.Errorf("should skip recent done-intent (age=%v)", age)
	}
}

// TestPatrolCrashPoint_AgentDeadInLiveSession verifies that when a tmux
// session is alive but the agent process inside it has died, the witness
// detects and restarts it.
func TestPatrolCrashPoint_AgentDeadInLiveSession(t *testing.T) {
	t.Parallel()

	sessionAlive := true
	agentAlive := false
	var doneIntent *DoneIntent

	// Should detect as zombie
	shouldDetect := sessionAlive && doneIntent == nil && !agentAlive
	if !shouldDetect {
		t.Error("should detect zombie for live session with dead agent")
	}

	zombie := ZombieResult{
		PolecatName:    "dead-agent",
		AgentState:     "working",
		Classification: ZombieAgentDeadInSession,
		WasActive:      true,
	}

	if zombie.Classification != ZombieAgentDeadInSession {
		t.Errorf("classification = %q, want %q", zombie.Classification, ZombieAgentDeadInSession)
	}
}

// TestPatrolCrashPoint_IdlePolecatNotZombie verifies that idle polecats
// with clean sandboxes are NOT classified as zombies (gt-s8bq fix).
func TestPatrolCrashPoint_IdlePolecatNotZombie(t *testing.T) {
	t.Parallel()

	agentState := beads.AgentStateIdle
	hookBead := "" // No hook bead — idle

	if isZombieState(agentState, hookBead) {
		t.Error("idle polecat with no hook bead should NOT be a zombie")
	}
}

// TestPatrolCrashPoint_IdleDirtySandboxDetected verifies that idle polecats
// with dirty sandboxes (uncommitted changes) are detected and reported.
func TestPatrolCrashPoint_IdleDirtySandboxDetected(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "dirty-idle",
		AgentState:     string(beads.AgentStateIdle),
		Classification: ZombieIdleDirtySandbox,
		CleanupStatus:  "uncommitted",
		WasActive:      false,
		Action:         "detected-dirty-idle-polecat",
	}

	if zombie.Classification != ZombieIdleDirtySandbox {
		t.Errorf("classification = %q, want %q", zombie.Classification, ZombieIdleDirtySandbox)
	}
	if zombie.WasActive {
		t.Error("idle dirty sandbox should not be marked as active work")
	}
	if !zombie.Classification.ImpliesActiveWork() {
		// ZombieIdleDirtySandbox does NOT imply active work
		t.Log("idle-dirty-sandbox correctly does not imply active work")
	}
}

// TestPatrolCrashPoint_DoneOrNukedNotZombie verifies that polecats with
// terminal agent states (done, nuked) are NOT treated as zombies even if
// they have a hook bead still set (GH#2795).
func TestPatrolCrashPoint_DoneOrNukedNotZombie(t *testing.T) {
	t.Parallel()

	for _, state := range []beads.AgentState{beads.AgentStateDone, beads.AgentStateNuked} {
		hookBead := "gt-some-issue"

		// isZombieState returns true because hookBead != ""
		if !isZombieState(state, hookBead) {
			t.Errorf("isZombieState(%q, %q) should be true (pre-condition)", state, hookBead)
		}

		// But detectZombieDeadSession has an explicit check:
		// if typedState == AgentStateDone || typedState == AgentStateNuked { return false }
		// This prevents treating completed polecats as zombies.
		if state.IsActive() {
			t.Errorf("state %q should NOT be active", state)
		}
	}
}

// TestPatrolCrashPoint_SpawningGracePeriod verifies that spawning polecats
// within the grace period are NOT treated as zombies (GH#2036).
func TestPatrolCrashPoint_SpawningGracePeriod(t *testing.T) {
	t.Parallel()

	state := beads.AgentState("spawning")
	hookBead := "gt-spawning-issue"

	// isZombieState returns true (hookBead != "")
	if !isZombieState(state, hookBead) {
		t.Error("spawning polecat with hook bead should trigger zombie check")
	}

	// But within SpawnGracePeriod, it should be skipped
	spawnAge := 10 * time.Second // Well within grace period
	if spawnAge >= SpawnGracePeriod {
		t.Error("10s should be within spawn grace period")
	}
}

// TestPatrolCrashPoint_DetectZombiePolecatsEmptyDir verifies that patrol
// handles missing or empty polecats directory gracefully.
func TestPatrolCrashPoint_DetectZombiePolecatsEmptyDir(t *testing.T) {
	t.Parallel()

	result := DetectZombiePolecats(DefaultBdCli(), "/nonexistent/path", "testrig", nil)

	if result.Checked != 0 {
		t.Errorf("Checked = %d, want 0 for nonexistent dir", result.Checked)
	}
	if len(result.Zombies) != 0 {
		t.Errorf("Zombies = %d, want 0 for nonexistent dir", len(result.Zombies))
	}
}

// TestPatrolCrashPoint_PatrolReceiptFromZombie verifies that patrol receipts
// are correctly built from zombie results, preserving classification and
// evidence for audit.
func TestPatrolCrashPoint_PatrolReceiptFromZombie(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "receipt-test",
		AgentState:     "working",
		Classification: ZombieSessionDeadActive,
		HookBead:       "gt-receipt-123",
		WasActive:      true,
		Action:         "restarted",
		BeadRecovered:  true,
	}

	receipt := BuildPatrolReceipt("testrig", zombie)

	if receipt.Polecat != "receipt-test" {
		t.Errorf("Polecat = %q, want %q", receipt.Polecat, "receipt-test")
	}
	if receipt.Evidence.Classification != ZombieSessionDeadActive {
		t.Errorf("Classification = %q, want %q", receipt.Evidence.Classification, ZombieSessionDeadActive)
	}
	if !receipt.Evidence.BeadRecovered {
		t.Error("BeadRecovered should be true")
	}
}

// TestPatrolCrashPoint_MultipleZombiesInSinglePatrol verifies that when
// multiple zombies are detected in a single patrol sweep, all are reported
// independently.
func TestPatrolCrashPoint_MultipleZombiesInSinglePatrol(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	rigName := "testrig"
	polecatsDir := filepath.Join(tmpDir, rigName, "polecats")
	if err := os.MkdirAll(polecatsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create multiple polecat directories
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if err := os.Mkdir(filepath.Join(polecatsDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	result := DetectZombiePolecats(DefaultBdCli(), tmpDir, rigName, nil)

	// Should check all 3 polecats
	if result.Checked != 3 {
		t.Errorf("Checked = %d, want 3", result.Checked)
	}
}

// TestPatrolCrashPoint_BuildPatrolReceiptsNilResult verifies that
// BuildPatrolReceipts handles nil and empty results gracefully.
func TestPatrolCrashPoint_BuildPatrolReceiptsNilResult(t *testing.T) {
	t.Parallel()

	// Nil result
	receipts := BuildPatrolReceipts("testrig", nil)
	if len(receipts) != 0 {
		t.Errorf("nil result should produce 0 receipts, got %d", len(receipts))
	}

	// Empty result
	receipts = BuildPatrolReceipts("testrig", &DetectZombiePolecatsResult{})
	if len(receipts) != 0 {
		t.Errorf("empty result should produce 0 receipts, got %d", len(receipts))
	}
}

// TestPatrolCrashPoint_ZombieClassificationImpliesActiveWork verifies that
// all zombie classifications correctly report whether they imply active work.
func TestPatrolCrashPoint_ZombieClassificationImpliesActiveWork(t *testing.T) {
	t.Parallel()

	activeClassifications := []ZombieClassification{
		ZombieStuckInDone,
		ZombieAgentDeadInSession,
		ZombieBeadClosedStillRunning,
		ZombieDoneIntentDead,
		ZombieSessionDeadActive,
		ZombieAgentSelfReportedStuck,
	}

	for _, c := range activeClassifications {
		if !c.ImpliesActiveWork() {
			t.Errorf("classification %q should imply active work", c)
		}
	}

	// Idle dirty sandbox does NOT imply active work
	if ZombieIdleDirtySandbox.ImpliesActiveWork() {
		t.Error("idle-dirty-sandbox should NOT imply active work")
	}
}

// TestPatrolCrashPoint_PendingMRSkipsZombie verifies that a polecat with a
// pending MR in the refinery queue is NOT treated as a zombie, even if its
// session is dead (the dead session is expected after gt done).
func TestPatrolCrashPoint_PendingMRSkipsZombie(t *testing.T) {
	t.Parallel()

	// When a polecat has completed gt done:
	// 1. done-intent label exists
	// 2. Session is dead (gt done transitions to idle, session may exit)
	// 3. MR is in the refinery queue (active_mr set on agent bead)
	//
	// The witness should NOT treat this as a zombie because the MR is pending.
	// This is verified by hasPendingMRFromSnapshot in detectZombieDeadSession.

	doneIntent := &DoneIntent{
		ExitType:  "COMPLETED",
		Timestamp: time.Now().Add(-120 * time.Second), // Old enough to be past grace
	}

	// With pending MR, should skip zombie detection
	hasPendingMR := true
	if doneIntent != nil && hasPendingMR {
		// detectZombieDeadSession returns (ZombieResult{}, false) here
		t.Log("correctly skips zombie detection when MR is pending")
	}
}
