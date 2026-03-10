package config

import (
	"testing"
)

func TestKiroPresetExists(t *testing.T) {
	preset := GetAgentPreset(AgentKiro)
	if preset == nil {
		t.Fatal("Kiro preset not found")
	}

	if preset.Command != "kiro-cli" {
		t.Errorf("expected command 'kiro-cli', got '%s'", preset.Command)
	}

	if len(preset.Args) == 0 || preset.Args[0] != "chat" {
		t.Errorf("expected args to start with 'chat', got %v", preset.Args)
	}

	if preset.ConfigDir != ".kiro" {
		t.Errorf("expected config dir '.kiro', got '%s'", preset.ConfigDir)
	}

	if preset.HooksProvider != "kiro" {
		t.Errorf("expected hooks provider 'kiro', got '%s'", preset.HooksProvider)
	}

	if !preset.HooksInformational {
		t.Error("expected HooksInformational to be true")
	}
}

func TestKiroInAgentList(t *testing.T) {
	agents := ListAgentPresets()

	found := false
	for _, name := range agents {
		if name == "kiro" {
			found = true
			break
		}
	}

	if !found {
		t.Error("kiro not found in agent preset list")
	}
}

func TestKiroRuntimeConfig(t *testing.T) {
	rc := RuntimeConfigFromPreset(AgentKiro)
	if rc == nil {
		t.Fatal("RuntimeConfigFromPreset returned nil")
	}

	if rc.Command != "kiro-cli" {
		t.Errorf("expected command 'kiro-cli', got '%s'", rc.Command)
	}

	if rc.Provider != "kiro" {
		t.Errorf("expected provider 'kiro', got '%s'", rc.Provider)
	}
}
