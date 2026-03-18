package witness

import (
	"fmt"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// Crash-point tests for witness patrol critical path (zombie detection).
//
// The witness patrol detects zombie polecats in various crash states:
//   1. done-intent: polecat set done-intent label, then crashed during gt done
//   2. agent-dead-in-session: tmux alive but agent process died
//   3. session-dead-active: tmux session dead but agent state still active
//
// These tests verify that zombie classification, verdict derivation, and
// the ImpliesActiveWork predicate correctly handle each crash state.

// TestZombieClassification_DoneIntentStuck verifies that a polecat stuck
// in gt done (done-intent label present, >60s old) is classified as
// ZombieStuckInDone with active work implied.
func TestZombieClassification_DoneIntentStuck(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "toast",
		AgentState:     "working",
		Classification: ZombieStuckInDone,
		HookBead:       "gs-abc123",
		WasActive:      true,
		Action:         "restarted-stuck-session (done-intent age=90s)",
	}

	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("ZombieStuckInDone should imply active work")
	}
	if !zombie.WasActive {
		t.Error("stuck-in-done polecat should be marked as active")
	}
}

// TestZombieClassification_DoneIntentDead verifies that a polecat whose
// session died while executing gt done is classified as ZombieDoneIntentDead.
func TestZombieClassification_DoneIntentDead(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "nux",
		AgentState:     "working",
		Classification: ZombieDoneIntentDead,
		HookBead:       "gs-xyz789",
		WasActive:      true,
		Action:         "restarted (done-intent age=5m, type=COMPLETED)",
	}

	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("ZombieDoneIntentDead should imply active work")
	}
}

// TestZombieClassification_AgentDeadInSession verifies that a polecat
// with a live tmux session but dead agent process is correctly classified.
func TestZombieClassification_AgentDeadInSession(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "furiosa",
		AgentState:     "running",
		Classification: ZombieAgentDeadInSession,
		HookBead:       "gs-def456",
		WasActive:      true,
		Action:         "restarted-agent-dead-session",
	}

	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("ZombieAgentDeadInSession should imply active work")
	}
}

// TestZombieClassification_SessionDeadActive verifies that a polecat
// with a dead session but active agent state is correctly classified.
func TestZombieClassification_SessionDeadActive(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "slit",
		AgentState:     "working",
		Classification: ZombieSessionDeadActive,
		HookBead:       "gs-ghi789",
		WasActive:      true,
		Action:         "restarted",
		BeadRecovered:  true,
	}

	if !zombie.Classification.ImpliesActiveWork() {
		t.Error("ZombieSessionDeadActive should imply active work")
	}
	if !zombie.BeadRecovered {
		t.Error("dead session zombie should have bead recovered for re-dispatch")
	}
}

// TestZombieClassification_IdleDirtySandbox verifies that an idle polecat
// with uncommitted changes is classified as ZombieIdleDirtySandbox and
// does NOT imply active work.
func TestZombieClassification_IdleDirtySandbox(t *testing.T) {
	t.Parallel()

	zombie := ZombieResult{
		PolecatName:    "rictus",
		AgentState:     "idle",
		Classification: ZombieIdleDirtySandbox,
		CleanupStatus:  "dirty",
		WasActive:      false,
		Action:         "detected-dirty-idle-polecat",
	}

	if zombie.Classification.ImpliesActiveWork() {
		t.Error("ZombieIdleDirtySandbox should NOT imply active work")
	}
	if zombie.WasActive {
		t.Error("idle dirty sandbox should not be marked as active")
	}
}

// TestImpliesActiveWork_AllClassifications verifies the ImpliesActiveWork
// predicate for every ZombieClassification value.
func TestImpliesActiveWork_AllClassifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		classification ZombieClassification
		wantActive     bool
	}{
		{ZombieStuckInDone, true},
		{ZombieAgentDeadInSession, true},
		{ZombieBeadClosedStillRunning, true},
		{ZombieDoneIntentDead, true},
		{ZombieSessionDeadActive, true},
		{ZombieAgentSelfReportedStuck, true},
		{ZombieIdleDirtySandbox, false},
		{ZombieClassification("unknown-future-type"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.classification), func(t *testing.T) {
			got := tt.classification.ImpliesActiveWork()
			if got != tt.wantActive {
				t.Errorf("ImpliesActiveWork() = %v, want %v", got, tt.wantActive)
			}
		})
	}
}

// TestExtractDoneIntent_ValidLabel verifies that extractDoneIntent correctly
// parses a well-formed done-intent label.
func TestExtractDoneIntent_ValidLabel(t *testing.T) {
	t.Parallel()

	now := time.Now()
	labels := []string{
		"gt:agent",
		"idle:3",
		"done-intent:COMPLETED:" + fmtUnixTS(now),
	}

	intent := extractDoneIntent(labels)
	if intent == nil {
		t.Fatal("expected non-nil DoneIntent")
	}
	if intent.ExitType != "COMPLETED" {
		t.Errorf("ExitType = %q, want %q", intent.ExitType, "COMPLETED")
	}
	// Timestamp should be within 1 second of now
	if diff := now.Sub(intent.Timestamp).Abs(); diff > time.Second {
		t.Errorf("timestamp diff = %v, want < 1s", diff)
	}
}

// TestExtractDoneIntent_NoLabel verifies that extractDoneIntent returns nil
// when no done-intent label is present.
func TestExtractDoneIntent_NoLabel(t *testing.T) {
	t.Parallel()

	labels := []string{"gt:agent", "idle:3", "done-cp:pushed:branch:12345"}
	if intent := extractDoneIntent(labels); intent != nil {
		t.Errorf("expected nil, got %+v", intent)
	}
}

// TestExtractDoneIntent_MalformedLabel verifies that extractDoneIntent
// returns nil for malformed done-intent labels.
func TestExtractDoneIntent_MalformedLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		label string
	}{
		{"missing timestamp", "done-intent:COMPLETED"},
		{"bad timestamp", "done-intent:COMPLETED:notanumber"},
		{"empty", "done-intent:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := []string{tt.label}
			if intent := extractDoneIntent(labels); intent != nil {
				t.Errorf("expected nil for malformed label %q, got %+v", tt.label, intent)
			}
		})
	}
}

// TestPatrolVerdict_CrashStates verifies that each zombie crash state
// produces the correct patrol verdict for the witness to act on.
func TestPatrolVerdict_CrashStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		classification ZombieClassification
		wasActive      bool
		wantVerdict    PatrolVerdict
	}{
		{"done-intent stuck", ZombieStuckInDone, true, PatrolVerdictStale},
		{"done-intent dead", ZombieDoneIntentDead, true, PatrolVerdictStale},
		{"agent dead in session", ZombieAgentDeadInSession, true, PatrolVerdictStale},
		{"session dead active", ZombieSessionDeadActive, true, PatrolVerdictStale},
		{"bead closed still running", ZombieBeadClosedStillRunning, true, PatrolVerdictStale},
		{"self-reported stuck", ZombieAgentSelfReportedStuck, true, PatrolVerdictStale},
		{"idle dirty sandbox", ZombieIdleDirtySandbox, false, PatrolVerdictOrphan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verdict := receiptVerdictForZombie(ZombieResult{
				Classification: tt.classification,
				WasActive:      tt.wasActive,
			})
			if verdict != tt.wantVerdict {
				t.Errorf("verdict = %q, want %q", verdict, tt.wantVerdict)
			}
		})
	}
}

// TestDetectZombieResult_MultipleZombieTypes verifies that a patrol sweep
// can detect multiple zombie types simultaneously and report them all.
func TestDetectZombieResult_MultipleZombieTypes(t *testing.T) {
	t.Parallel()

	result := &DetectZombiePolecatsResult{
		Checked: 4,
		Zombies: []ZombieResult{
			{PolecatName: "alpha", Classification: ZombieStuckInDone, WasActive: true},
			{PolecatName: "bravo", Classification: ZombieAgentDeadInSession, WasActive: true},
			{PolecatName: "charlie", Classification: ZombieIdleDirtySandbox, WasActive: false},
		},
	}

	if result.Checked != 4 {
		t.Errorf("Checked = %d, want 4", result.Checked)
	}
	if len(result.Zombies) != 3 {
		t.Fatalf("Zombies = %d, want 3", len(result.Zombies))
	}

	// Verify each zombie has the correct classification
	classifications := map[string]ZombieClassification{
		"alpha":   ZombieStuckInDone,
		"bravo":   ZombieAgentDeadInSession,
		"charlie": ZombieIdleDirtySandbox,
	}
	for _, z := range result.Zombies {
		want, ok := classifications[z.PolecatName]
		if !ok {
			t.Errorf("unexpected zombie: %s", z.PolecatName)
			continue
		}
		if z.Classification != want {
			t.Errorf("%s: classification = %q, want %q", z.PolecatName, z.Classification, want)
		}
	}
}

// TestPatrolReceipt_CrashStateEvidence verifies that patrol receipts
// include the correct evidence fields for crash-state zombies.
func TestPatrolReceipt_CrashStateEvidence(t *testing.T) {
	t.Parallel()

	receipt := BuildPatrolReceipt("gastown", ZombieResult{
		PolecatName:    "toast",
		AgentState:     "working",
		Classification: ZombieDoneIntentDead,
		HookBead:       "gs-6au",
		WasActive:      true,
		Action:         "restarted (done-intent age=3m, type=COMPLETED)",
	})

	if receipt.Verdict != PatrolVerdictStale {
		t.Errorf("Verdict = %q, want %q", receipt.Verdict, PatrolVerdictStale)
	}
	if receipt.Polecat != "toast" {
		t.Errorf("Polecat = %q, want %q", receipt.Polecat, "toast")
	}
	if receipt.Evidence.HookBead != "gs-6au" {
		t.Errorf("Evidence.HookBead = %q, want %q", receipt.Evidence.HookBead, "gs-6au")
	}
	if receipt.Evidence.AgentState != "working" {
		t.Errorf("Evidence.AgentState = %q, want %q", receipt.Evidence.AgentState, "working")
	}
}

// TestAgentBeadSnapshot_CleanupStatus verifies that the cleanup status
// is correctly extracted from the agent bead snapshot.
func TestAgentBeadSnapshot_CleanupStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		snap   *agentBeadSnapshot
		want   string
	}{
		{"nil snapshot", nil, ""},
		{"nil fields", &agentBeadSnapshot{}, ""},
		{"clean", &agentBeadSnapshot{Fields: &beads.AgentFields{CleanupStatus: "clean"}}, "clean"},
		{"dirty", &agentBeadSnapshot{Fields: &beads.AgentFields{CleanupStatus: "dirty"}}, "dirty"},
		{"unpushed", &agentBeadSnapshot{Fields: &beads.AgentFields{CleanupStatus: "unpushed"}}, "unpushed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.snap.cleanupStatus()
			if got != tt.want {
				t.Errorf("cleanupStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAgentBeadSnapshot_Age verifies that snapshot age calculation handles
// edge cases (nil, empty, unparseable timestamps).
func TestAgentBeadSnapshot_Age(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		snap    *agentBeadSnapshot
		wantMax time.Duration
	}{
		{"nil snapshot", nil, 25 * time.Hour},
		{"empty updated_at", &agentBeadSnapshot{UpdatedAt: ""}, 25 * time.Hour},
		{"unparseable", &agentBeadSnapshot{UpdatedAt: "not-a-date"}, 25 * time.Hour},
		{"recent RFC3339", &agentBeadSnapshot{UpdatedAt: time.Now().Format(time.RFC3339)}, 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age := tt.snap.age()
			if age > tt.wantMax {
				t.Errorf("age() = %v, want <= %v", age, tt.wantMax)
			}
		})
	}
}

// fmtUnixTS formats a time as a Unix timestamp string for test labels.
func fmtUnixTS(t time.Time) string {
	return fmt.Sprintf("%d", t.Unix())
}
