package tmux

import "time"

// Client abstracts tmux operations for testing. Consumers should depend on this
// interface rather than *Tmux directly so that a FakeTmux can be injected in
// unit tests.
//
// The interface covers the core session-management surface used by polecat,
// witness, deacon, nudge, and other packages. Methods that are purely
// configuration or UI (themes, bindings, display messages) are intentionally
// excluded — add them as needed.
type Client interface {
	// Session lifecycle
	NewSession(name, workDir string) error
	NewSessionWithCommand(name, workDir, command string) error
	NewSessionWithCommandAndEnv(name, workDir, command string, env map[string]string) error
	KillSession(name string) error
	KillSessionWithProcesses(name string) error
	HasSession(name string) (bool, error)
	ListSessions() ([]string, error)
	RenameSession(oldName, newName string) error

	// Keys / input
	SendKeys(session, keys string) error
	SendKeysRaw(session, keys string) error

	// Pane inspection
	CapturePane(session string, lines int) (string, error)
	CapturePaneAll(session string) (string, error)
	GetPanePID(target string) (string, error)
	GetPaneID(session string) (string, error)

	// Session inspection
	GetSessionInfo(name string) (*SessionInfo, error)
	IsAgentAlive(session string) bool
	ServerPID() int

	// Environment
	SetEnvironment(session, key, value string) error
	GetEnvironment(session, key string) (string, error)

	// Session configuration
	SetRemainOnExit(pane string, on bool) error
	ConfigureGasTownSession(session string, theme Theme, rig, worker, role string) error
	SetAutoRespawnHook(session string) error

	// Startup dialogs
	AcceptStartupDialogs(session string) error
	AcceptWorkspaceTrustDialog(session string) error
	AcceptBypassPermissionsWarning(session string) error

	// Wait helpers
	WaitForCommand(session string, excludeCommands []string, timeout time.Duration) error
}

// Verify *Tmux satisfies Client at compile time.
var _ Client = (*Tmux)(nil)
