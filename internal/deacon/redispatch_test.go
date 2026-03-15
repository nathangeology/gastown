package deacon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseRecoveredBeadSubject(t *testing.T) {
	tests := []struct {
		subject string
		wantID  string
		wantOK  bool
	}{
		{"RECOVERED_BEAD gt-abc123", "gt-abc123", true},
		{"RECOVERED_BEAD bd-xyz", "bd-xyz", true},
		{"RECOVERED_BEAD   gt-abc123  ", "gt-abc123", true},
		{"RECOVERED_BEAD", "", false},
		{"RECOVERED_BEAD ", "", false},
		{"MERGE_READY foo", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			gotID, gotOK := ParseRecoveredBeadSubject(tt.subject)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("ParseRecoveredBeadSubject(%q) = (%q, %v), want (%q, %v)",
					tt.subject, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestParseRecoveredBeadBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantRig string
	}{
		{
			name: "standard format",
			body: `Recovered abandoned bead from dead polecat.

Bead: gt-abc123
Polecat: gastown/max
Previous Status: hooked

The bead has been reset to open with no assignee.`,
			wantRig: "gastown",
		},
		{
			name:    "no polecat line",
			body:    "Some other body content",
			wantRig: "",
		},
		{
			name: "different rig",
			body: `Bead: bd-xyz
Polecat: beads/alpha
Previous Status: in_progress`,
			wantRig: "beads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRecoveredBeadBody(tt.body)
			if got != tt.wantRig {
				t.Errorf("ParseRecoveredBeadBody() = %q, want %q", got, tt.wantRig)
			}
		})
	}
}

func TestRedispatchState_LoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	deaconDir := filepath.Join(tmpDir, "deacon")
	if err := os.MkdirAll(deaconDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Test empty state
	state, err := LoadRedispatchState(tmpDir)
	if err != nil {
		t.Fatalf("LoadRedispatchState: %v", err)
	}
	if len(state.Beads) != 0 {
		t.Errorf("expected empty beads, got %d", len(state.Beads))
	}

	// Add some state
	beadState := state.GetBeadState("gt-abc")
	beadState.RecordAttempt("gastown")
	beadState.RecordAttempt("gastown")

	if err := SaveRedispatchState(tmpDir, state); err != nil {
		t.Fatalf("SaveRedispatchState: %v", err)
	}

	// Reload
	loaded, err := LoadRedispatchState(tmpDir)
	if err != nil {
		t.Fatalf("LoadRedispatchState after save: %v", err)
	}

	loadedBead := loaded.GetBeadState("gt-abc")
	if loadedBead.AttemptCount != 2 {
		t.Errorf("expected 2 attempts, got %d", loadedBead.AttemptCount)
	}
	if loadedBead.LastRig != "gastown" {
		t.Errorf("expected LastRig=gastown, got %q", loadedBead.LastRig)
	}
}

func TestBeadRedispatchState_Cooldown(t *testing.T) {
	state := &BeadRedispatchState{BeadID: "gt-test"}

	// Not in cooldown initially
	if state.IsInCooldown(5 * time.Minute) {
		t.Error("expected not in cooldown initially")
	}

	// Record attempt puts in cooldown
	state.RecordAttempt("gastown")
	if !state.IsInCooldown(5 * time.Minute) {
		t.Error("expected in cooldown after attempt")
	}

	remaining := state.CooldownRemaining(5 * time.Minute)
	if remaining <= 0 || remaining > 5*time.Minute {
		t.Errorf("expected cooldown remaining in (0, 5m], got %v", remaining)
	}

	// Not in cooldown with 0 duration
	if state.IsInCooldown(0) {
		t.Error("expected not in cooldown with 0 duration")
	}
}

func TestBeadRedispatchState_ShouldEscalate(t *testing.T) {
	state := &BeadRedispatchState{BeadID: "gt-test"}

	if state.ShouldEscalate(3) {
		t.Error("should not escalate with 0 attempts")
	}

	state.AttemptCount = 2
	if state.ShouldEscalate(3) {
		t.Error("should not escalate with 2/3 attempts")
	}

	state.AttemptCount = 3
	if !state.ShouldEscalate(3) {
		t.Error("should escalate with 3/3 attempts")
	}

	state.AttemptCount = 5
	if !state.ShouldEscalate(3) {
		t.Error("should escalate with 5/3 attempts")
	}
}

func TestBeadRedispatchState_Escalation(t *testing.T) {
	state := &BeadRedispatchState{BeadID: "gt-test"}

	if state.Escalated {
		t.Error("should not be escalated initially")
	}

	state.RecordEscalation()

	if !state.Escalated {
		t.Error("should be escalated after RecordEscalation")
	}
	if state.EscalatedAt.IsZero() {
		t.Error("EscalatedAt should be set")
	}
}

func TestRedispatchState_GetBeadState(t *testing.T) {
	state := &RedispatchState{}

	// GetBeadState creates map if nil
	bead := state.GetBeadState("gt-new")
	if bead == nil {
		t.Fatal("expected non-nil bead state")
	}
	if bead.BeadID != "gt-new" {
		t.Errorf("expected BeadID=gt-new, got %q", bead.BeadID)
	}

	// Second call returns same object
	bead2 := state.GetBeadState("gt-new")
	if bead != bead2 {
		t.Error("expected same bead state object on second call")
	}

	// Different bead returns different object
	bead3 := state.GetBeadState("gt-other")
	if bead == bead3 {
		t.Error("expected different bead state for different ID")
	}
}

func TestRedispatch_RedispatchesOpenBead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell script mocks")
	}

	townRoot := t.TempDir()
	binDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	routes := `{"prefix":"gt-","path":"gastown"}` + "\n"
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(routes), 0o644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	slingLog := filepath.Join(townRoot, "sling.log")
	bdScript := `#!/bin/sh
if [ "$1" = "show" ]; then
  echo '[{"status":"open"}]'
  exit 0
fi
echo "unexpected bd args: $*" >&2
exit 1
`
	gtScript := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "sling" ]; then
  echo "$@" >> "%s"
  exit 0
fi
echo "unexpected gt args: $*" >&2
exit 1
`, slingLog)
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(bdScript), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gt"), []byte(gtScript), 0o755); err != nil {
		t.Fatalf("write fake gt: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := Redispatch(townRoot, "gt-abc123", "", 3, 0)
	if result.Action != "redispatched" {
		t.Fatalf("action = %q, want %q (err=%v)", result.Action, "redispatched", result.Error)
	}
	if result.TargetRig != "gastown" {
		t.Fatalf("target rig = %q, want %q", result.TargetRig, "gastown")
	}
	if result.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", result.Attempts)
	}

	logData, err := os.ReadFile(slingLog)
	if err != nil {
		t.Fatalf("read sling log: %v", err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "sling gt-abc123 gastown --force --no-convoy") {
		t.Fatalf("unexpected sling invocation log: %q", logText)
	}
}

func TestRedispatch_EscalatesAfterMaxAttempts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell script mocks")
	}

	townRoot := t.TempDir()
	binDir := t.TempDir()
	mailLog := filepath.Join(townRoot, "mail.log")

	state := &RedispatchState{}
	bead := state.GetBeadState("gt-abc123")
	bead.AttemptCount = 3
	bead.LastRig = "gastown"
	bead.LastAttemptTime = time.Now().Add(-2 * DefaultRedispatchCooldown)
	if err := SaveRedispatchState(townRoot, state); err != nil {
		t.Fatalf("save redispatch state: %v", err)
	}

	gtScript := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "mail" ] && [ "$2" = "send" ]; then
  echo "$@" >> "%s"
  exit 0
fi
echo "unexpected gt args: $*" >&2
exit 1
`, mailLog)
	if err := os.WriteFile(filepath.Join(binDir, "gt"), []byte(gtScript), 0o755); err != nil {
		t.Fatalf("write fake gt: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := Redispatch(townRoot, "gt-abc123", "gastown", 3, 0)
	if result.Action != "escalated" {
		t.Fatalf("action = %q, want %q (err=%v)", result.Action, "escalated", result.Error)
	}
	if result.Error != nil {
		t.Fatalf("unexpected escalation error: %v", result.Error)
	}
	if !strings.Contains(result.Message, "escalated to Mayor after 3 failed re-dispatches") {
		t.Fatalf("message = %q, want escalation summary", result.Message)
	}

	logData, err := os.ReadFile(mailLog)
	if err != nil {
		t.Fatalf("read mail log: %v", err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "mail send mayor/") {
		t.Fatalf("expected mayor escalation mail, got %q", logText)
	}
	if !strings.Contains(logText, "REDISPATCH_FAILED: gt-abc123 (3 attempts)") {
		t.Fatalf("expected redispatch failure subject, got %q", logText)
	}
}
