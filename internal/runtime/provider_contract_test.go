package runtime_test

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/runtime"
)

type providerContract struct {
	name                  string
	agentName             string
	rc                    *config.RuntimeConfig
	role                  string
	sessionEnvName        string
	sessionEnvValue       string
	wantPromptContains    bool
	wantResumeCommand     string
	wantFallbackCommands  []string
	wantFallbackInfo      runtime.StartupFallbackInfo
	wantReadyPromptPrefix string
	wantReadyDelayMs      int
	wantPaneCommands      []string
}

func TestProviderContracts(t *testing.T) {
	for _, tc := range []providerContract{
		{
			name:                  "claude-like",
			agentName:             "claude",
			rc:                    config.RuntimeConfigFromPreset(config.AgentClaude),
			role:                  "polecat",
			sessionEnvName:        "CLAUDE_SESSION_ID",
			sessionEnvValue:       "claude-session-123",
			wantPromptContains:    true,
			wantResumeCommand:     "claude --dangerously-skip-permissions --resume session-123",
			wantFallbackCommands:  nil,
			wantFallbackInfo:      runtime.StartupFallbackInfo{},
			wantReadyPromptPrefix: "❯ ",
			wantReadyDelayMs:      10000,
			wantPaneCommands:      []string{"node", "claude"},
		},
		{
			name:                  "codex-like",
			agentName:             "codex",
			rc:                    config.RuntimeConfigFromPreset(config.AgentCodex),
			role:                  "polecat",
			wantPromptContains:    false,
			wantResumeCommand:     "codex resume session-123 --dangerously-bypass-approvals-and-sandbox",
			wantFallbackCommands:  []string{"gt prime && gt mail check --inject"},
			wantFallbackInfo:      runtime.StartupFallbackInfo{IncludePrimeInBeacon: true, SendBeaconNudge: true, SendStartupNudge: true, StartupNudgeDelayMs: runtime.DefaultPrimeWaitMs},
			wantReadyPromptPrefix: "",
			wantReadyDelayMs:      3000,
			wantPaneCommands:      []string{"codex"},
		},
		{
			name:      "generic",
			agentName: "generic-agent",
			role:      "polecat",
			rc: &config.RuntimeConfig{
				Provider:   "generic",
				Command:    "generic-agent",
				Args:       []string{"--autonomous"},
				PromptMode: "arg",
				Session: &config.RuntimeSessionConfig{
					SessionIDEnv: "GENERIC_SESSION_ID",
				},
				Hooks: &config.RuntimeHooksConfig{
					Provider: "none",
				},
				Tmux: &config.RuntimeTmuxConfig{
					ProcessNames:      []string{"generic-agent"},
					ReadyPromptPrefix: "READY> ",
					ReadyDelayMs:      1500,
				},
				Instructions: &config.RuntimeInstructionsConfig{
					File: "AGENTS.md",
				},
			},
			sessionEnvName:        "GENERIC_SESSION_ID",
			sessionEnvValue:       "generic-session-123",
			wantPromptContains:    true,
			wantResumeCommand:     "",
			wantFallbackCommands:  []string{"gt prime && gt mail check --inject"},
			wantFallbackInfo:      runtime.StartupFallbackInfo{IncludePrimeInBeacon: true, SendStartupNudge: true, StartupNudgeDelayMs: runtime.DefaultPrimeWaitMs},
			wantReadyPromptPrefix: "READY> ",
			wantReadyDelayMs:      1500,
			wantPaneCommands:      []string{"generic-agent"},
		},
		{
			name:      "informational-hooks-role-aware-fallback",
			agentName: "copilot",
			role:      "crew",
			rc: &config.RuntimeConfig{
				Provider:   "copilot",
				Command:    "copilot",
				PromptMode: "arg",
				Hooks: &config.RuntimeHooksConfig{
					Provider:      "copilot",
					Informational: true,
				},
				Tmux: &config.RuntimeTmuxConfig{
					ProcessNames: []string{"copilot"},
					ReadyDelayMs: 2500,
				},
			},
			wantPromptContains:    true,
			wantResumeCommand:     "copilot --yolo --resume session-123",
			wantFallbackCommands:  []string{"gt prime"},
			wantFallbackInfo:      runtime.StartupFallbackInfo{IncludePrimeInBeacon: true, SendStartupNudge: true, StartupNudgeDelayMs: runtime.DefaultPrimeWaitMs},
			wantReadyPromptPrefix: "",
			wantReadyDelayMs:      2500,
			wantPaneCommands:      []string{"copilot"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assertReadinessContract(t, tc)
			assertStartupFallbackContract(t, tc)
			assertSessionCaptureContract(t, tc)
			assertStartupEnvContract(t, tc)
			assertResumeAndHandoffContract(t, tc)
		})
	}
}

func assertReadinessContract(t *testing.T, tc providerContract) {
	t.Helper()

	if tc.rc.Tmux == nil {
		t.Fatalf("%s: runtime config missing tmux settings", tc.name)
	}
	if tc.rc.Tmux.ReadyPromptPrefix != tc.wantReadyPromptPrefix {
		t.Fatalf("%s: ReadyPromptPrefix = %q, want %q", tc.name, tc.rc.Tmux.ReadyPromptPrefix, tc.wantReadyPromptPrefix)
	}
	if tc.rc.Tmux.ReadyDelayMs != tc.wantReadyDelayMs {
		t.Fatalf("%s: ReadyDelayMs = %d, want %d", tc.name, tc.rc.Tmux.ReadyDelayMs, tc.wantReadyDelayMs)
	}

	gotPaneCommands := config.ExpectedPaneCommands(tc.rc)
	if strings.Join(gotPaneCommands, ",") != strings.Join(tc.wantPaneCommands, ",") {
		t.Fatalf("%s: ExpectedPaneCommands = %v, want %v", tc.name, gotPaneCommands, tc.wantPaneCommands)
	}
}

func assertStartupFallbackContract(t *testing.T, tc providerContract) {
	t.Helper()

	gotCommands := runtime.StartupFallbackCommands(tc.role, tc.rc)
	if strings.Join(gotCommands, "\n") != strings.Join(tc.wantFallbackCommands, "\n") {
		t.Fatalf("%s: StartupFallbackCommands = %v, want %v", tc.name, gotCommands, tc.wantFallbackCommands)
	}

	gotInfo := runtime.GetStartupFallbackInfo(tc.rc)
	if *gotInfo != tc.wantFallbackInfo {
		t.Fatalf("%s: GetStartupFallbackInfo = %+v, want %+v", tc.name, *gotInfo, tc.wantFallbackInfo)
	}

	withMinDelay := runtime.RuntimeConfigWithMinDelay(tc.rc, runtime.DefaultPrimeWaitMs)
	if withMinDelay.Tmux == nil {
		t.Fatalf("%s: RuntimeConfigWithMinDelay should always return tmux settings", tc.name)
	}
	if withMinDelay.Tmux.ReadyDelayMs < runtime.DefaultPrimeWaitMs {
		t.Fatalf("%s: RuntimeConfigWithMinDelay ReadyDelayMs = %d, want >= %d", tc.name, withMinDelay.Tmux.ReadyDelayMs, runtime.DefaultPrimeWaitMs)
	}
	if withMinDelay.Tmux.ReadyPromptPrefix != "" {
		t.Fatalf("%s: RuntimeConfigWithMinDelay should clear ReadyPromptPrefix, got %q", tc.name, withMinDelay.Tmux.ReadyPromptPrefix)
	}
}

func assertSessionCaptureContract(t *testing.T, tc providerContract) {
	t.Helper()

	for _, key := range []string{"GT_SESSION_ID_ENV", "GT_AGENT", "CLAUDE_SESSION_ID", "GENERIC_SESSION_ID"} {
		t.Setenv(key, "")
	}

	if tc.agentName != "" {
		t.Setenv("GT_AGENT", tc.agentName)
	}
	if tc.sessionEnvName != "" {
		t.Setenv(tc.sessionEnvName, tc.sessionEnvValue)
	}
	if tc.name == "generic" {
		t.Setenv("GT_SESSION_ID_ENV", tc.sessionEnvName)
	}

	got := runtime.SessionIDFromEnv()
	want := tc.sessionEnvValue
	if tc.sessionEnvName == "" {
		want = ""
	}
	if got != want {
		t.Fatalf("%s: SessionIDFromEnv = %q, want %q", tc.name, got, want)
	}
}

func assertStartupEnvContract(t *testing.T, tc providerContract) {
	t.Helper()

	env := config.AgentEnv(config.AgentEnvConfig{
		Role:         tc.role,
		Rig:          "gastown",
		AgentName:    "furiosa",
		TownRoot:     "/tmp/town-root",
		SessionIDEnv: tc.sessionEnvName,
		Agent:        tc.agentName,
		SessionName:  "gt-furiosa",
	})

	if got := env["GT_ROOT"]; got != "/tmp/town-root" {
		t.Fatalf("%s: GT_ROOT = %q, want %q", tc.name, got, "/tmp/town-root")
	}
	if got := env["GT_SESSION"]; got != "gt-furiosa" {
		t.Fatalf("%s: GT_SESSION = %q, want %q", tc.name, got, "gt-furiosa")
	}
	if tc.sessionEnvName == "" {
		if _, ok := env["GT_SESSION_ID_ENV"]; ok {
			t.Fatalf("%s: GT_SESSION_ID_ENV should be omitted for runtimes without env-based session IDs", tc.name)
		}
	} else if got := env["GT_SESSION_ID_ENV"]; got != tc.sessionEnvName {
		t.Fatalf("%s: GT_SESSION_ID_ENV = %q, want %q", tc.name, got, tc.sessionEnvName)
	}

	cmd := config.BuildStartupCommandWithEnv(env, tc.rc.BuildCommandWithPrompt("handoff instructions"), "")
	if !strings.HasPrefix(cmd, "export ") {
		t.Fatalf("%s: startup command should export env, got %q", tc.name, cmd)
	}
	if strings.Contains(cmd, "cd ") || strings.Contains(cmd, "/tmp/worktree") {
		t.Fatalf("%s: startup command should not encode cwd handling, got %q", tc.name, cmd)
	}
}

func assertResumeAndHandoffContract(t *testing.T, tc providerContract) {
	t.Helper()

	handoffCmd := tc.rc.BuildCommandWithPrompt("handoff instructions")
	hasPrompt := strings.Contains(handoffCmd, "handoff instructions")
	if hasPrompt != tc.wantPromptContains {
		t.Fatalf("%s: BuildCommandWithPrompt prompt inclusion = %v, want %v (%q)", tc.name, hasPrompt, tc.wantPromptContains, handoffCmd)
	}

	resumeCmd := config.BuildResumeCommand(tc.agentName, "session-123")
	if resumeCmd != tc.wantResumeCommand {
		t.Fatalf("%s: BuildResumeCommand = %q, want %q", tc.name, resumeCmd, tc.wantResumeCommand)
	}
	if resumeCmd != "" && strings.Contains(resumeCmd, "handoff instructions") {
		t.Fatalf("%s: resume command should not embed fresh-start handoff prompt, got %q", tc.name, resumeCmd)
	}
}
