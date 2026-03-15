package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/runtime"
	"github.com/steveyegge/gastown/internal/session"
)

func TestSessionDispatchContracts(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		role                    string
		agentName               string
		rc                      *config.RuntimeConfig
		beacon                  session.BeaconConfig
		wantBeaconContains      []string
		wantBeaconNotContains   []string
		wantFallbackCommands    []string
		wantStartupNudgeContent string
	}{
		{
			name:      "sling-startup-with-hooks-and-prompt",
			role:      "polecat",
			agentName: "claude",
			rc:        config.RuntimeConfigFromPreset(config.AgentClaude),
			beacon: session.BeaconConfig{
				Recipient: "polecat furiosa (rig: gastown)",
				Sender:    "witness",
				Topic:     "assigned",
				MolID:     "gs-6ss",
			},
			wantBeaconContains: []string{"assigned:gs-6ss", "gt prime --hook", "begin work"},
			wantBeaconNotContains: []string{
				"Run `gt prime` to initialize your context.",
			},
			wantFallbackCommands:    nil,
			wantStartupNudgeContent: runtime.StartupNudgeContent(),
		},
		{
			name:      "fallback-priming-for-non-hook-agent",
			role:      "polecat",
			agentName: "codex",
			rc:        config.RuntimeConfigFromPreset(config.AgentCodex),
			beacon: session.BeaconConfig{
				Recipient:               "polecat dementus (rig: gastown)",
				Sender:                  "witness",
				Topic:                   "assigned",
				IncludePrimeInstruction: true,
			},
			wantBeaconContains: []string{"gt prime", "assigned"},
			wantBeaconNotContains: []string{
				"begin work",
			},
			wantFallbackCommands:    []string{"gt prime && gt mail check --inject"},
			wantStartupNudgeContent: runtime.StartupNudgeContent(),
		},
		{
			name:      "restart-beacon-is-continuation-not-fresh-start",
			role:      "polecat",
			agentName: "claude",
			rc:        config.RuntimeConfigFromPreset(config.AgentClaude),
			beacon: session.BeaconConfig{
				Recipient: "polecat toast (rig: gastown)",
				Sender:    "witness",
				Topic:     "restart",
			},
			wantBeaconContains: []string{"restart"},
			wantBeaconNotContains: []string{
				"gt prime",
				"begin work",
			},
			wantFallbackCommands:    nil,
			wantStartupNudgeContent: runtime.StartupNudgeContent(),
		},
		{
			name:      "crew-readiness-uses-prime-only-fallback",
			role:      "crew",
			agentName: "copilot",
			rc: &config.RuntimeConfig{
				Provider:   "copilot",
				Command:    "copilot",
				PromptMode: "arg",
				Hooks: &config.RuntimeHooksConfig{
					Provider:      "copilot",
					Informational: true,
				},
				Tmux: &config.RuntimeTmuxConfig{
					ProcessNames:      []string{"copilot"},
					ReadyPromptPrefix: "READY> ",
					ReadyDelayMs:      1500,
				},
			},
			beacon: session.BeaconConfig{
				Recipient:               "crew max (rig: gastown)",
				Sender:                  "human",
				Topic:                   "start",
				IncludePrimeInstruction: true,
			},
			wantBeaconContains:      []string{"start", "gt prime"},
			wantBeaconNotContains:   []string{"begin work"},
			wantFallbackCommands:    []string{"gt prime"},
			wantStartupNudgeContent: runtime.StartupNudgeContent(),
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			beacon := session.FormatStartupBeacon(tc.beacon)
			for _, want := range tc.wantBeaconContains {
				if !strings.Contains(beacon, want) {
					t.Fatalf("beacon missing %q:\n%s", want, beacon)
				}
			}
			for _, unwanted := range tc.wantBeaconNotContains {
				if strings.Contains(beacon, unwanted) {
					t.Fatalf("beacon should not contain %q:\n%s", unwanted, beacon)
				}
			}

			gotFallback := runtime.StartupFallbackCommands(tc.role, tc.rc)
			if strings.Join(gotFallback, "\n") != strings.Join(tc.wantFallbackCommands, "\n") {
				t.Fatalf("StartupFallbackCommands(%s) = %v, want %v", tc.role, gotFallback, tc.wantFallbackCommands)
			}

			if got := runtime.StartupNudgeContent(); got != tc.wantStartupNudgeContent {
				t.Fatalf("StartupNudgeContent() = %q, want %q", got, tc.wantStartupNudgeContent)
			}

			withMinDelay := runtime.RuntimeConfigWithMinDelay(tc.rc, runtime.DefaultPrimeWaitMs)
			if withMinDelay.Tmux == nil || withMinDelay.Tmux.ReadyDelayMs < runtime.DefaultPrimeWaitMs {
				t.Fatalf("RuntimeConfigWithMinDelay() should enforce minimum delay, got %+v", withMinDelay.Tmux)
			}
			if withMinDelay.Tmux.ReadyPromptPrefix != "" {
				t.Fatalf("RuntimeConfigWithMinDelay() should clear ReadyPromptPrefix, got %q", withMinDelay.Tmux.ReadyPromptPrefix)
			}
		})
	}
}
