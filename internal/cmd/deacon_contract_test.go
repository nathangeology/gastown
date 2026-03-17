package cmd

import (
	"encoding/json"
	"testing"
	"time"
)

// Contract tests for deacon commands.

func TestDeaconCmd_HasExpectedSubcommands(t *testing.T) {
	expected := []string{
		"start", "stop", "attach", "status", "restart",
		"heartbeat", "health-check", "force-kill", "health-state",
		"stale-hooks", "pause", "resume",
		"cleanup-orphans", "zombie-scan",
		"redispatch", "redispatch-state",
		"feed-stranded", "feed-stranded-state",
	}
	for _, name := range expected {
		found := false
		for _, sub := range deaconCmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("deacon missing subcommand %q", name)
		}
	}
}

func TestDeaconCmd_RequiresSubcommand(t *testing.T) {
	if deaconCmd.RunE == nil {
		t.Fatal("deacon cmd should have RunE set")
	}
}

func TestDeaconCmd_HasAlias(t *testing.T) {
	found := false
	for _, a := range deaconCmd.Aliases {
		if a == "dea" {
			found = true
		}
	}
	if !found {
		t.Error("deacon cmd should have 'dea' alias")
	}
}

func TestDeaconCmd_GroupID(t *testing.T) {
	if deaconCmd.GroupID != GroupAgents {
		t.Errorf("GroupID = %q, want %q", deaconCmd.GroupID, GroupAgents)
	}
}

func TestDeaconStartCmd_Flags(t *testing.T) {
	f := deaconStartCmd.Flags().Lookup("agent")
	if f == nil {
		t.Fatal("start cmd missing --agent flag")
	}
	if f.DefValue != "" {
		t.Errorf("--agent default = %q, want empty", f.DefValue)
	}
}

func TestDeaconStartCmd_HasSpawnAlias(t *testing.T) {
	found := false
	for _, a := range deaconStartCmd.Aliases {
		if a == "spawn" {
			found = true
		}
	}
	if !found {
		t.Error("start cmd should have 'spawn' alias")
	}
}

func TestDeaconAttachCmd_HasAlias(t *testing.T) {
	found := false
	for _, a := range deaconAttachCmd.Aliases {
		if a == "at" {
			found = true
		}
	}
	if !found {
		t.Error("attach cmd should have 'at' alias")
	}
}

func TestDeaconStatusCmd_Flags(t *testing.T) {
	f := deaconStatusCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("status cmd missing --json flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--json default = %q, want %q", f.DefValue, "false")
	}
}

func TestDeaconHealthCheckCmd_Flags(t *testing.T) {
	tests := []struct {
		flag     string
		defValue string
	}{
		{"timeout", "30s"},
		{"failures", "3"},
		{"cooldown", "5m0s"},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := deaconHealthCheckCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("missing flag --%s", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("default = %q, want %q", f.DefValue, tt.defValue)
			}
		})
	}
}

func TestDeaconHealthCheckCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := deaconHealthCheckCmd.Args(deaconHealthCheckCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := deaconHealthCheckCmd.Args(deaconHealthCheckCmd, []string{"agent1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
	if err := deaconHealthCheckCmd.Args(deaconHealthCheckCmd, []string{"a", "b"}); err == nil {
		t.Error("should reject 2 args")
	}
}

func TestDeaconForceKillCmd_Flags(t *testing.T) {
	for _, flag := range []string{"reason", "skip-notify"} {
		if deaconForceKillCmd.Flags().Lookup(flag) == nil {
			t.Errorf("force-kill cmd missing --%s flag", flag)
		}
	}
}

func TestDeaconForceKillCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := deaconForceKillCmd.Args(deaconForceKillCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := deaconForceKillCmd.Args(deaconForceKillCmd, []string{"agent1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestDeaconStaleHooksCmd_Flags(t *testing.T) {
	tests := []struct {
		flag     string
		defValue string
	}{
		{"max-age", "1h0m0s"},
		{"dry-run", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := deaconStaleHooksCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("missing flag --%s", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("default = %q, want %q", f.DefValue, tt.defValue)
			}
		})
	}
}

func TestDeaconZombieScanCmd_Flags(t *testing.T) {
	if deaconZombieScanCmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("zombie-scan cmd missing --dry-run flag")
	}
}

func TestDeaconRedispatchCmd_Flags(t *testing.T) {
	for _, flag := range []string{"rig", "max-attempts", "cooldown"} {
		if deaconRedispatchCmd.Flags().Lookup(flag) == nil {
			t.Errorf("redispatch cmd missing --%s flag", flag)
		}
	}
}

func TestDeaconRedispatchCmd_RequiresExactlyOneArg(t *testing.T) {
	if err := deaconRedispatchCmd.Args(deaconRedispatchCmd, []string{}); err == nil {
		t.Error("should reject 0 args")
	}
	if err := deaconRedispatchCmd.Args(deaconRedispatchCmd, []string{"bead-1"}); err != nil {
		t.Errorf("should accept 1 arg, got: %v", err)
	}
}

func TestDeaconFeedStrandedCmd_Flags(t *testing.T) {
	for _, flag := range []string{"max-feeds", "cooldown", "json"} {
		if deaconFeedStrandedCmd.Flags().Lookup(flag) == nil {
			t.Errorf("feed-stranded cmd missing --%s flag", flag)
		}
	}
}

func TestDeaconPauseCmd_Flags(t *testing.T) {
	if deaconPauseCmd.Flags().Lookup("reason") == nil {
		t.Fatal("pause cmd missing --reason flag")
	}
}

func TestDeaconStatusOutput_JSONSchema(t *testing.T) {
	now := time.Now().UTC()
	out := DeaconStatusOutput{
		Running: true,
		Paused:  false,
		Session: "gt-deacon",
		Heartbeat: &HeartbeatStatus{
			Timestamp:  now,
			AgeSec:     10.0,
			Cycle:      5,
			LastAction: "patrol",
			Fresh:      true,
			Stale:      false,
			VeryStale:  false,
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"running", "paused", "session", "heartbeat"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestDeaconStatusOutput_HeartbeatOmittedWhenNil(t *testing.T) {
	out := DeaconStatusOutput{
		Running: false,
		Session: "gt-deacon",
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := m["heartbeat"]; ok {
		t.Error("heartbeat should be omitted when nil")
	}
}

func TestAgentAddressToIDs_ValidFormats(t *testing.T) {
	tests := []struct {
		address string
		wantErr bool
	}{
		{"deacon", false},
		{"mayor", false},
		{"gastown/witness", false},
		{"gastown/refinery", false},
		{"gastown/polecats/max", false},
		{"gastown/crew/alpha", false},
		{"gastown/unknown", true},
		{"invalid", true},
		{"a/b/c/d", true},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			beadID, sessionName, err := agentAddressToIDs(tt.address)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.address)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if beadID == "" {
				t.Error("beadID should not be empty")
			}
			if sessionName == "" {
				t.Error("sessionName should not be empty")
			}
		})
	}
}

func TestDeaconRestartCmd_Flags(t *testing.T) {
	if deaconRestartCmd.Flags().Lookup("agent") == nil {
		t.Fatal("restart cmd missing --agent flag")
	}
}

func TestDeaconZombieScanCmd_SuggestFor(t *testing.T) {
	found := false
	for _, s := range deaconZombieScanCmd.SuggestFor {
		if s == "orphan-scan" {
			found = true
		}
	}
	if !found {
		t.Error("zombie-scan should suggest 'orphan-scan'")
	}
}
