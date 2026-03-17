package cmd

import (
	"encoding/json"
	"testing"
)

// Contract tests for refinery commands.

func TestRefineryCmd_HasExpectedSubcommands(t *testing.T) {
	expected := []string{
		"start", "stop", "status", "queue", "attach", "restart",
		"claim", "release", "unclaimed", "ready", "blocked",
	}
	for _, name := range expected {
		found := false
		for _, sub := range refineryCmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("refinery missing subcommand %q", name)
		}
	}
}

func TestRefineryCmd_RequiresSubcommand(t *testing.T) {
	if refineryCmd.RunE == nil {
		t.Fatal("refinery cmd should have RunE set")
	}
}

func TestRefineryCmd_HasAlias(t *testing.T) {
	found := false
	for _, a := range refineryCmd.Aliases {
		if a == "ref" {
			found = true
		}
	}
	if !found {
		t.Error("refinery cmd should have 'ref' alias")
	}
}

func TestRefineryStartCmd_Flags(t *testing.T) {
	tests := []struct {
		flag     string
		defValue string
	}{
		{"foreground", "false"},
		{"agent", ""},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := refineryStartCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("missing flag --%s", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("default = %q, want %q", f.DefValue, tt.defValue)
			}
		})
	}
}

func TestRefineryStartCmd_AcceptsOptionalArg(t *testing.T) {
	if err := refineryStartCmd.Args(refineryStartCmd, []string{}); err != nil {
		t.Errorf("should accept 0 args, got: %v", err)
	}
	if err := refineryStartCmd.Args(refineryStartCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
	if err := refineryStartCmd.Args(refineryStartCmd, []string{"a", "b"}); err == nil {
		t.Error("should reject 2 args")
	}
}

func TestRefineryStopCmd_AcceptsOptionalArg(t *testing.T) {
	if err := refineryStopCmd.Args(refineryStopCmd, []string{}); err != nil {
		t.Errorf("should accept 0 args, got: %v", err)
	}
	if err := refineryStopCmd.Args(refineryStopCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestRefineryStatusCmd_Flags(t *testing.T) {
	if refineryStatusCmd.Flags().Lookup("json") == nil {
		t.Fatal("status cmd missing --json flag")
	}
}

func TestRefineryQueueCmd_Flags(t *testing.T) {
	if refineryQueueCmd.Flags().Lookup("json") == nil {
		t.Fatal("queue cmd missing --json flag")
	}
}

func TestRefineryClaimCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := refineryClaimCmd.Args(refineryClaimCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := refineryClaimCmd.Args(refineryClaimCmd, []string{"mr-1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
	if err := refineryClaimCmd.Args(refineryClaimCmd, []string{"a", "b"}); err == nil {
		t.Error("should reject 2 args")
	}
}

func TestRefineryReleaseCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := refineryReleaseCmd.Args(refineryReleaseCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := refineryReleaseCmd.Args(refineryReleaseCmd, []string{"mr-1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestRefineryReadyCmd_Flags(t *testing.T) {
	for _, flag := range []string{"json", "all"} {
		if refineryReadyCmd.Flags().Lookup(flag) == nil {
			t.Errorf("ready cmd missing --%s flag", flag)
		}
	}
}

func TestRefineryBlockedCmd_Flags(t *testing.T) {
	if refineryBlockedCmd.Flags().Lookup("json") == nil {
		t.Fatal("blocked cmd missing --json flag")
	}
}

func TestRefineryUnclaimedCmd_Flags(t *testing.T) {
	if refineryUnclaimedCmd.Flags().Lookup("json") == nil {
		t.Fatal("unclaimed cmd missing --json flag")
	}
}

func TestRefineryStatusOutput_JSONSchema(t *testing.T) {
	out := RefineryStatusOutput{
		Running:     true,
		RigName:     "gastown",
		Session:     "gt-gastown-refinery",
		QueueLength: 3,
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"running", "rig_name", "session", "queue_length"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}
	if m["queue_length"].(float64) != 3 {
		t.Errorf("queue_length = %v, want 3", m["queue_length"])
	}
}

func TestRefineryStatusOutput_Roundtrip(t *testing.T) {
	original := RefineryStatusOutput{
		Running:     true,
		RigName:     "gastown",
		Session:     "gt-gastown-refinery",
		QueueLength: 5,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded RefineryStatusOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded != original {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestRefineryCmd_GroupID(t *testing.T) {
	if refineryCmd.GroupID != GroupAgents {
		t.Errorf("GroupID = %q, want %q", refineryCmd.GroupID, GroupAgents)
	}
}

func TestRefineryStartCmd_HasSpawnAlias(t *testing.T) {
	found := false
	for _, a := range refineryStartCmd.Aliases {
		if a == "spawn" {
			found = true
		}
	}
	if !found {
		t.Error("start cmd should have 'spawn' alias")
	}
}

func TestGetWorkerID_Default(t *testing.T) {
	t.Setenv("GT_REFINERY_WORKER", "")
	id := getWorkerID()
	if id != "refinery-1" {
		t.Errorf("getWorkerID() = %q, want %q", id, "refinery-1")
	}
}

func TestGetWorkerID_Override(t *testing.T) {
	t.Setenv("GT_REFINERY_WORKER", "refinery-2")
	id := getWorkerID()
	if id != "refinery-2" {
		t.Errorf("getWorkerID() = %q, want %q", id, "refinery-2")
	}
}
