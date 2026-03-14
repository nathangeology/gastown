package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/tmux"
)

func TestResetReusedPolecatSession_KillsExistingSession(t *testing.T) {
	oldHasSession := polecatSpawnHasSession
	oldKillSession := polecatSpawnKillSession
	t.Cleanup(func() {
		polecatSpawnHasSession = oldHasSession
		polecatSpawnKillSession = oldKillSession
	})

	var killed []string
	polecatSpawnHasSession = func(_ *tmux.Tmux, sessionName string) (bool, error) {
		if sessionName != "p3-furiosa" {
			t.Fatalf("sessionName = %q, want p3-furiosa", sessionName)
		}
		return true, nil
	}
	polecatSpawnKillSession = func(_ *tmux.Tmux, sessionName string) error {
		killed = append(killed, sessionName)
		return nil
	}

	if err := resetReusedPolecatSession(nil, "p3-furiosa"); err != nil {
		t.Fatalf("resetReusedPolecatSession() error = %v", err)
	}
	if len(killed) != 1 || killed[0] != "p3-furiosa" {
		t.Fatalf("killed sessions = %#v, want [\"p3-furiosa\"]", killed)
	}
}

func TestResetReusedPolecatSession_NoSessionNoop(t *testing.T) {
	oldHasSession := polecatSpawnHasSession
	oldKillSession := polecatSpawnKillSession
	t.Cleanup(func() {
		polecatSpawnHasSession = oldHasSession
		polecatSpawnKillSession = oldKillSession
	})

	polecatSpawnHasSession = func(_ *tmux.Tmux, _ string) (bool, error) { return false, nil }
	polecatSpawnKillSession = func(_ *tmux.Tmux, _ string) error {
		t.Fatal("kill should not be called when no session exists")
		return nil
	}

	if err := resetReusedPolecatSession(nil, "p3-furiosa"); err != nil {
		t.Fatalf("resetReusedPolecatSession() error = %v", err)
	}
}

func TestResetReusedPolecatSession_PropagatesKillError(t *testing.T) {
	oldHasSession := polecatSpawnHasSession
	oldKillSession := polecatSpawnKillSession
	t.Cleanup(func() {
		polecatSpawnHasSession = oldHasSession
		polecatSpawnKillSession = oldKillSession
	})

	polecatSpawnHasSession = func(_ *tmux.Tmux, _ string) (bool, error) { return true, nil }
	polecatSpawnKillSession = func(_ *tmux.Tmux, _ string) error { return errors.New("tmux busy") }

	err := resetReusedPolecatSession(nil, "p3-furiosa")
	if err == nil {
		t.Fatal("resetReusedPolecatSession() expected error")
	}
	if !strings.Contains(err.Error(), "tmux busy") {
		t.Fatalf("error = %v, want tmux busy", err)
	}
}
