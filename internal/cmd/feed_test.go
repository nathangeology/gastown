package cmd

import (
	"testing"
)

func TestFeedCmd_FlagsExist(t *testing.T) {
	flags := []struct {
		name     string
		defValue string
	}{
		{"follow", "false"},
		{"no-follow", "false"},
		{"limit", "100"},
		{"since", ""},
		{"mol", ""},
		{"type", ""},
		{"rig", ""},
		{"window", "false"},
		{"plain", "false"},
		{"problems", "false"},
	}
	for _, f := range flags {
		flag := feedCmd.Flags().Lookup(f.name)
		if flag == nil {
			t.Errorf("--%s flag should exist", f.name)
			continue
		}
		if flag.DefValue != f.defValue {
			t.Errorf("--%s default = %q, want %q", f.name, flag.DefValue, f.defValue)
		}
	}
}

func TestFeedCmd_IsRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "feed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("feed command should be registered with rootCmd")
	}
}

func TestFeedCmd_HasCorrectGroup(t *testing.T) {
	if feedCmd.GroupID != GroupDiag {
		t.Errorf("feed should be in diag group, got %s", feedCmd.GroupID)
	}
}

func TestBuildFeedArgs_Defaults(t *testing.T) {
	// Save and restore global state
	oldFollow, oldNoFollow, oldLimit := feedFollow, feedNoFollow, feedLimit
	oldSince, oldMol, oldType, oldRig := feedSince, feedMol, feedType, feedRig
	defer func() {
		feedFollow, feedNoFollow, feedLimit = oldFollow, oldNoFollow, oldLimit
		feedSince, feedMol, feedType, feedRig = oldSince, oldMol, oldType, oldRig
	}()

	feedFollow = false
	feedNoFollow = false
	feedLimit = 100
	feedSince = ""
	feedMol = ""
	feedType = ""
	feedRig = ""

	args := buildFeedArgs()
	// Default: follow is true (unless non-TTY), limit is 100 (not added)
	// In test context stdout is not a TTY, so follow is disabled
	for _, arg := range args {
		if arg == "--limit" {
			t.Error("--limit should not appear when set to default 100")
		}
	}
}

func TestBuildFeedArgs_WithFilters(t *testing.T) {
	oldFollow, oldNoFollow, oldLimit := feedFollow, feedNoFollow, feedLimit
	oldSince, oldMol, oldType, oldRig := feedSince, feedMol, feedType, feedRig
	defer func() {
		feedFollow, feedNoFollow, feedLimit = oldFollow, oldNoFollow, oldLimit
		feedSince, feedMol, feedType, feedRig = oldSince, oldMol, oldType, oldRig
	}()

	feedFollow = true
	feedNoFollow = false
	feedLimit = 50
	feedSince = "1h"
	feedMol = "gt-abc"
	feedType = "create"
	feedRig = "greenplace"

	args := buildFeedArgs()

	expected := map[string]string{
		"--follow": "",
		"--limit":  "50",
		"--since":  "1h",
		"--mol":    "gt-abc",
		"--type":   "create",
		"--rig":    "greenplace",
	}

	for i := 0; i < len(args); i++ {
		if val, ok := expected[args[i]]; ok {
			if val != "" {
				if i+1 >= len(args) || args[i+1] != val {
					t.Errorf("expected %s %s in args", args[i], val)
				}
			}
			delete(expected, args[i])
		}
	}
	for flag := range expected {
		t.Errorf("expected %s in args, not found", flag)
	}
}

func TestBuildFeedArgs_NoFollowOverridesFollow(t *testing.T) {
	oldFollow, oldNoFollow, oldLimit := feedFollow, feedNoFollow, feedLimit
	oldSince, oldMol, oldType, oldRig := feedSince, feedMol, feedType, feedRig
	defer func() {
		feedFollow, feedNoFollow, feedLimit = oldFollow, oldNoFollow, oldLimit
		feedSince, feedMol, feedType, feedRig = oldSince, oldMol, oldType, oldRig
	}()

	feedFollow = false
	feedNoFollow = true
	feedLimit = 100
	feedSince = ""
	feedMol = ""
	feedType = ""
	feedRig = ""

	args := buildFeedArgs()
	for _, arg := range args {
		if arg == "--follow" {
			t.Error("--follow should not appear when --no-follow is set")
		}
	}
}
