package cmd

import (
	"testing"
)

// Contract tests for witness commands.

func TestWitnessCmd_HasExpectedSubcommands(t *testing.T) {
	expected := []string{"start", "stop", "status", "attach", "restart"}
	for _, name := range expected {
		found := false
		for _, sub := range witnessCmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("witness missing subcommand %q", name)
		}
	}
}

func TestWitnessCmd_RequiresSubcommand(t *testing.T) {
	if witnessCmd.RunE == nil {
		t.Fatal("witness cmd should have RunE set")
	}
}

func TestWitnessStartCmd_Flags(t *testing.T) {
	tests := []struct {
		flag     string
		defValue string
	}{
		{"foreground", "false"},
		{"agent", ""},
		{"env", "[]"},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := witnessStartCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("missing flag --%s", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("default = %q, want %q", f.DefValue, tt.defValue)
			}
		})
	}
}

func TestWitnessStartCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := witnessStartCmd.Args(witnessStartCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := witnessStartCmd.Args(witnessStartCmd, []string{"a", "b"}); err == nil {
		t.Error("should reject 2 args")
	}
	if err := witnessStartCmd.Args(witnessStartCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestWitnessStopCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := witnessStopCmd.Args(witnessStopCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := witnessStopCmd.Args(witnessStopCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestWitnessStatusCmd_Flags(t *testing.T) {
	f := witnessStatusCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("status cmd missing --json flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--json default = %q, want %q", f.DefValue, "false")
	}
}

func TestWitnessStatusCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := witnessStatusCmd.Args(witnessStatusCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := witnessStatusCmd.Args(witnessStatusCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestWitnessAttachCmd_AcceptsOptionalArg(t *testing.T) {
	if err := witnessAttachCmd.Args(witnessAttachCmd, []string{}); err != nil {
		t.Errorf("should accept 0 args, got: %v", err)
	}
	if err := witnessAttachCmd.Args(witnessAttachCmd, []string{"rig1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
	if err := witnessAttachCmd.Args(witnessAttachCmd, []string{"a", "b"}); err == nil {
		t.Error("should reject 2 args")
	}
}

func TestWitnessStatusOutput_JSONFields(t *testing.T) {
	out := WitnessStatusOutput{
		Running:           true,
		RigName:           "gastown",
		Session:           "gt-gastown-witness",
		MonitoredPolecats: []string{"rictus", "nux"},
	}
	if !out.Running {
		t.Error("Running should be true")
	}
	if len(out.MonitoredPolecats) != 2 {
		t.Errorf("MonitoredPolecats len = %d, want 2", len(out.MonitoredPolecats))
	}
}

func TestWitnessRestartCmd_Flags(t *testing.T) {
	for _, flag := range []string{"agent", "env"} {
		if witnessRestartCmd.Flags().Lookup(flag) == nil {
			t.Errorf("restart cmd missing --%s flag", flag)
		}
	}
}

func TestWitnessCmd_GroupID(t *testing.T) {
	if witnessCmd.GroupID != GroupAgents {
		t.Errorf("GroupID = %q, want %q", witnessCmd.GroupID, GroupAgents)
	}
}

func TestWitnessStartCmd_HasSpawnAlias(t *testing.T) {
	found := false
	for _, a := range witnessStartCmd.Aliases {
		if a == "spawn" {
			found = true
		}
	}
	if !found {
		t.Error("start cmd should have 'spawn' alias")
	}
}
