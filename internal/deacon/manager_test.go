package deacon

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/tmux"
)

// newTestManager creates a Manager backed by a FakeTmux for testing.
func newTestManager(townRoot string, fake *tmux.FakeTmux) *Manager {
	return &Manager{
		townRoot: townRoot,
		tmux:     fake,
	}
}

// fakeTmuxWith returns a FakeTmux with the given sessions pre-populated.
func fakeTmuxWith(sessions ...string) *tmux.FakeTmux {
	f := &tmux.FakeTmux{Sessions: make(map[string]bool)}
	for _, s := range sessions {
		f.Sessions[s] = true
	}
	return f
}

func TestNewManager(t *testing.T) {
	m := NewManager("/tmp/test-town")
	if m.townRoot != "/tmp/test-town" {
		t.Errorf("townRoot = %q, want %q", m.townRoot, "/tmp/test-town")
	}
	if m.tmux == nil {
		t.Error("tmux should not be nil")
	}
}

func TestManager_SessionName(t *testing.T) {
	m := NewManager("/tmp/test-town")
	name := m.SessionName()
	if name == "" {
		t.Error("SessionName() should not be empty")
	}
	if name != SessionName() {
		t.Errorf("method SessionName() = %q, package SessionName() = %q", name, SessionName())
	}
}

func TestManager_deaconDir(t *testing.T) {
	m := NewManager("/tmp/test-town")
	expected := filepath.Join("/tmp/test-town", "deacon")
	if m.deaconDir() != expected {
		t.Errorf("deaconDir() = %q, want %q", m.deaconDir(), expected)
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	fake := fakeTmuxWith("hq-deacon")
	fake.AgentAlive = map[string]bool{"hq-deacon": true}
	m := newTestManager(t.TempDir(), fake)

	err := m.Start("")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Start() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestStart_ZombieDetected_KillFails(t *testing.T) {
	// Session exists but agent is dead (zombie). Kill will fail.
	fake := fakeTmuxWith("hq-deacon")
	fake.Err = errors.New("kill failed: session locked")
	m := newTestManager(t.TempDir(), fake)

	err := m.Start("")
	if err == nil {
		t.Fatal("Start() should return error when zombie kill fails")
	}
}

func TestStart_ZombieDetected_KillSucceeds(t *testing.T) {
	// Zombie kill succeeds, Start continues into config/runtime.
	fake := fakeTmuxWith("hq-deacon")
	// AgentAlive not set → IsAgentAlive returns false → zombie detected
	m := newTestManager(t.TempDir(), fake)

	_ = m.Start("")

	// Verify KillSessionWithProcesses was called
	found := false
	for _, c := range fake.Calls {
		if c.Method == "KillSessionWithProcesses" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected KillSessionWithProcesses call for zombie cleanup")
	}
}

func TestStart_NoExistingSession(t *testing.T) {
	fake := &tmux.FakeTmux{}
	m := newTestManager(t.TempDir(), fake)

	_ = m.Start("")

	// Should NOT have tried to kill anything
	for _, c := range fake.Calls {
		if c.Method == "KillSessionWithProcesses" {
			t.Error("should not kill when no session exists")
		}
	}
}

func TestStop_NotRunning(t *testing.T) {
	fake := &tmux.FakeTmux{}
	m := newTestManager(t.TempDir(), fake)

	err := m.Stop()
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("Stop() error = %v, want ErrNotRunning", err)
	}
}

func TestStop_HasSessionError(t *testing.T) {
	fake := &tmux.FakeTmux{Err: errors.New("tmux server crashed")}
	m := newTestManager(t.TempDir(), fake)

	err := m.Stop()
	if err == nil {
		t.Fatal("Stop() should return error when HasSession fails")
	}
}

func TestStop_Success(t *testing.T) {
	fake := fakeTmuxWith("hq-deacon")
	m := newTestManager(t.TempDir(), fake)

	err := m.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
	found := false
	for _, c := range fake.Calls {
		if c.Method == "KillSessionWithProcesses" {
			found = true
		}
	}
	if !found {
		t.Error("expected KillSessionWithProcesses call")
	}
}

func TestIsRunning(t *testing.T) {
	tests := []struct {
		name    string
		fake    *tmux.FakeTmux
		wantRun bool
		wantErr bool
	}{
		{
			name:    "running",
			fake:    fakeTmuxWith("hq-deacon"),
			wantRun: true,
		},
		{
			name:    "not running",
			fake:    &tmux.FakeTmux{},
			wantRun: false,
		},
		{
			name:    "error",
			fake:    &tmux.FakeTmux{Err: errors.New("tmux error")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t.TempDir(), tc.fake)
			running, err := m.IsRunning()
			if (err != nil) != tc.wantErr {
				t.Errorf("IsRunning() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if running != tc.wantRun {
				t.Errorf("IsRunning() = %v, want %v", running, tc.wantRun)
			}
		})
	}
}

func TestStatus_NotRunning(t *testing.T) {
	fake := &tmux.FakeTmux{}
	m := newTestManager(t.TempDir(), fake)

	info, err := m.Status()
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("Status() error = %v, want ErrNotRunning", err)
	}
	if info != nil {
		t.Error("Status() should return nil info when not running")
	}
}

func TestStatus_Running(t *testing.T) {
	expected := &tmux.SessionInfo{Name: "hq-deacon", Windows: 1}
	fake := fakeTmuxWith("hq-deacon")
	fake.SessionInfos = map[string]*tmux.SessionInfo{"hq-deacon": expected}
	m := newTestManager(t.TempDir(), fake)

	info, err := m.Status()
	if err != nil {
		t.Errorf("Status() error = %v", err)
	}
	if info != expected {
		t.Errorf("Status() = %v, want %v", info, expected)
	}
}
