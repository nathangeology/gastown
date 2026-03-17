package tmux

import (
	"fmt"
	"sync"
	"time"
)

// Call records a single method invocation on FakeTmux.
type Call struct {
	Method string
	Args   []interface{}
}

// FakeTmux is an in-memory test double that implements Client.
// It records every call and returns configurable responses.
//
// Usage:
//
//	fake := &tmux.FakeTmux{}
//	fake.Sessions["my-session"] = true
//	ok, _ := fake.HasSession("my-session") // true
//	fake.Calls // [{Method:"HasSession", Args:["my-session"]}]
type FakeTmux struct {
	mu sync.Mutex

	// Calls records every method invocation in order.
	Calls []Call

	// Sessions tracks which sessions "exist". Methods like HasSession,
	// KillSession, NewSession, and ListSessions consult this map.
	Sessions map[string]bool

	// CaptureOutput maps session name → pane content returned by CapturePane/CapturePaneAll.
	CaptureOutput map[string]string

	// PanePIDs maps target → PID string returned by GetPanePID.
	PanePIDs map[string]string

	// PaneIDs maps session → pane ID returned by GetPaneID.
	PaneIDs map[string]string

	// Environments maps "session/key" → value for Get/SetEnvironment.
	Environments map[string]string

	// SessionInfos maps session name → *SessionInfo for GetSessionInfo.
	SessionInfos map[string]*SessionInfo

	// AgentAlive maps session → alive bool for IsAgentAlive.
	AgentAlive map[string]bool

	// Pid is the value returned by ServerPID.
	Pid int

	// Err, when non-nil, is returned by every method that returns an error.
	// For finer control, set per-method error maps instead.
	Err error
}

// Verify FakeTmux satisfies Client at compile time.
var _ Client = (*FakeTmux)(nil)

func (f *FakeTmux) record(method string, args ...interface{}) {
	f.Calls = append(f.Calls, Call{Method: method, Args: args})
}

func (f *FakeTmux) NewSession(name, workDir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("NewSession", name, workDir)
	if f.Err != nil {
		return f.Err
	}
	if f.Sessions == nil {
		f.Sessions = make(map[string]bool)
	}
	f.Sessions[name] = true
	return nil
}

func (f *FakeTmux) NewSessionWithCommand(name, workDir, command string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("NewSessionWithCommand", name, workDir, command)
	if f.Err != nil {
		return f.Err
	}
	if f.Sessions == nil {
		f.Sessions = make(map[string]bool)
	}
	f.Sessions[name] = true
	return nil
}

func (f *FakeTmux) NewSessionWithCommandAndEnv(name, workDir, command string, env map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("NewSessionWithCommandAndEnv", name, workDir, command, env)
	if f.Err != nil {
		return f.Err
	}
	if f.Sessions == nil {
		f.Sessions = make(map[string]bool)
	}
	f.Sessions[name] = true
	return nil
}

func (f *FakeTmux) KillSession(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("KillSession", name)
	if f.Err != nil {
		return f.Err
	}
	delete(f.Sessions, name)
	return nil
}

func (f *FakeTmux) KillSessionWithProcesses(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("KillSessionWithProcesses", name)
	if f.Err != nil {
		return f.Err
	}
	delete(f.Sessions, name)
	return nil
}

func (f *FakeTmux) HasSession(name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("HasSession", name)
	if f.Err != nil {
		return false, f.Err
	}
	return f.Sessions[name], nil
}

func (f *FakeTmux) ListSessions() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListSessions")
	if f.Err != nil {
		return nil, f.Err
	}
	var names []string
	for name, alive := range f.Sessions {
		if alive {
			names = append(names, name)
		}
	}
	return names, nil
}

func (f *FakeTmux) RenameSession(oldName, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("RenameSession", oldName, newName)
	if f.Err != nil {
		return f.Err
	}
	if !f.Sessions[oldName] {
		return ErrSessionNotFound
	}
	delete(f.Sessions, oldName)
	f.Sessions[newName] = true
	return nil
}

func (f *FakeTmux) SendKeys(session, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("SendKeys", session, keys)
	return f.Err
}

func (f *FakeTmux) SendKeysRaw(session, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("SendKeysRaw", session, keys)
	return f.Err
}

func (f *FakeTmux) CapturePane(session string, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CapturePane", session, lines)
	if f.Err != nil {
		return "", f.Err
	}
	return f.CaptureOutput[session], nil
}

func (f *FakeTmux) CapturePaneAll(session string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CapturePaneAll", session)
	if f.Err != nil {
		return "", f.Err
	}
	return f.CaptureOutput[session], nil
}

func (f *FakeTmux) GetPanePID(target string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetPanePID", target)
	if f.Err != nil {
		return "", f.Err
	}
	pid, ok := f.PanePIDs[target]
	if !ok {
		return "", fmt.Errorf("no pane PID for %s", target)
	}
	return pid, nil
}

func (f *FakeTmux) GetPaneID(session string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetPaneID", session)
	if f.Err != nil {
		return "", f.Err
	}
	id, ok := f.PaneIDs[session]
	if !ok {
		return "%0", nil // sensible default
	}
	return id, nil
}

func (f *FakeTmux) GetSessionInfo(name string) (*SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetSessionInfo", name)
	if f.Err != nil {
		return nil, f.Err
	}
	info, ok := f.SessionInfos[name]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return info, nil
}

func (f *FakeTmux) IsAgentAlive(session string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("IsAgentAlive", session)
	return f.AgentAlive[session]
}

func (f *FakeTmux) ServerPID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ServerPID")
	return f.Pid
}

func (f *FakeTmux) SetEnvironment(session, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("SetEnvironment", session, key, value)
	if f.Err != nil {
		return f.Err
	}
	if f.Environments == nil {
		f.Environments = make(map[string]string)
	}
	f.Environments[session+"/"+key] = value
	return nil
}

func (f *FakeTmux) GetEnvironment(session, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetEnvironment", session, key)
	if f.Err != nil {
		return "", f.Err
	}
	return f.Environments[session+"/"+key], nil
}

func (f *FakeTmux) SetRemainOnExit(_ string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("SetRemainOnExit")
	return f.Err
}

func (f *FakeTmux) ConfigureGasTownSession(session string, _ Theme, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ConfigureGasTownSession", session)
	return f.Err
}

func (f *FakeTmux) SetAutoRespawnHook(session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("SetAutoRespawnHook", session)
	return f.Err
}

func (f *FakeTmux) AcceptStartupDialogs(session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("AcceptStartupDialogs", session)
	return f.Err
}

func (f *FakeTmux) AcceptWorkspaceTrustDialog(session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("AcceptWorkspaceTrustDialog", session)
	return f.Err
}

func (f *FakeTmux) AcceptBypassPermissionsWarning(session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("AcceptBypassPermissionsWarning", session)
	return f.Err
}

func (f *FakeTmux) WaitForCommand(_ string, _ []string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("WaitForCommand")
	return f.Err
}
