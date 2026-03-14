package refinery

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/testutil"
	"github.com/steveyegge/gastown/internal/tmux"
)

type mockRefineryTmux struct {
	hasSessionResult bool
	agentAlive       bool
	sessionInfo      *tmux.SessionInfo
	sessionInfoErr   error
	waitErr          error
	newSessionErr    error

	newSessionCalls int
	killCalls       []string
	nudges          []string
}

func (m *mockRefineryTmux) HasSession(_ string) (bool, error) { return m.hasSessionResult, nil }
func (m *mockRefineryTmux) IsAgentAlive(_ string) bool        { return m.agentAlive }
func (m *mockRefineryTmux) CheckSessionHealth(_ string, _ time.Duration) tmux.ZombieStatus {
	if m.hasSessionResult && m.agentAlive {
		return tmux.SessionHealthy
	}
	return tmux.SessionDead
}
func (m *mockRefineryTmux) GetSessionInfo(_ string) (*tmux.SessionInfo, error) {
	return m.sessionInfo, m.sessionInfoErr
}
func (m *mockRefineryTmux) KillSession(name string) error {
	m.killCalls = append(m.killCalls, name)
	return nil
}
func (m *mockRefineryTmux) KillSessionWithProcesses(name string) error {
	m.killCalls = append(m.killCalls, name)
	return nil
}
func (m *mockRefineryTmux) NewSessionWithCommand(_, _, _ string) error {
	m.newSessionCalls++
	return m.newSessionErr
}
func (m *mockRefineryTmux) SetEnvironment(_, _, _ string) error { return nil }
func (m *mockRefineryTmux) ConfigureGasTownSession(_ string, _ tmux.Theme, _, _, _ string) error {
	return nil
}
func (m *mockRefineryTmux) AcceptStartupDialogs(_ string) error { return nil }
func (m *mockRefineryTmux) WaitForRuntimeReady(_ string, _ *config.RuntimeConfig, _ time.Duration) error {
	return m.waitErr
}
func (m *mockRefineryTmux) NudgeSession(_, message string) error {
	m.nudges = append(m.nudges, message)
	return nil
}

func setupTestRegistry(t *testing.T) {
	t.Helper()
	// Use a prefix that won't collide with real gastown sessions.
	// The "tr" prefix conflicts with actual rigs running on the host
	// (e.g., tr-refinery, tr-witness), causing tests that assert
	// "no session exists" to fail in gastown workspaces.
	reg := session.NewPrefixRegistry()
	reg.Register("xut", "testrig")
	old := session.DefaultRegistry()
	session.SetDefaultRegistry(reg)
	t.Cleanup(func() { session.SetDefaultRegistry(old) })
}

func setupTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	setupTestRegistry(t)

	// Create temp directory structure
	tmpDir := t.TempDir()
	rigPath := filepath.Join(tmpDir, "testrig")
	if err := os.MkdirAll(filepath.Join(rigPath, ".runtime"), 0755); err != nil {
		t.Fatalf("mkdir .runtime: %v", err)
	}

	r := &rig.Rig{
		Name: "testrig",
		Path: rigPath,
	}

	return NewManager(r), rigPath
}

func setupMockManager(t *testing.T, mock *mockRefineryTmux) *Manager {
	t.Helper()
	mgr, _ := setupTestManager(t)
	mgr.tmux = mock
	return mgr
}

func TestManager_SessionName(t *testing.T) {
	mgr, _ := setupTestManager(t)

	want := "xut-refinery"
	got := mgr.SessionName()
	if got != want {
		t.Errorf("SessionName() = %s, want %s", got, want)
	}
}

func TestManager_Start_NudgesRefineryPatrol(t *testing.T) {
	mock := &mockRefineryTmux{}
	mgr := setupMockManager(t, mock)

	err := mgr.Start(false, "codex")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if mock.newSessionCalls != 1 {
		t.Fatalf("expected 1 session creation, got %d", mock.newSessionCalls)
	}
	if len(mock.nudges) == 0 {
		t.Fatal("expected refinery startup to send at least one nudge")
	}
	foundPatrol := false
	for _, nudge := range mock.nudges {
		if strings.Contains(nudge, "gt refinery ready --all --json") && strings.Contains(nudge, "merge it to `main` if clear") {
			foundPatrol = true
			break
		}
	}
	if !foundPatrol {
		t.Fatalf("expected refinery patrol nudge, got %#v", mock.nudges)
	}
}

func TestManager_Start_KillsSessionWhenRuntimeReadyFails(t *testing.T) {
	mock := &mockRefineryTmux{waitErr: errors.New("runtime never became ready")}
	mgr := setupMockManager(t, mock)

	err := mgr.Start(false, "codex")
	if err == nil {
		t.Fatal("Start() should return runtime readiness error")
	}
	if len(mock.killCalls) == 0 {
		t.Fatal("expected cleanup kill after runtime readiness failure")
	}
}

func TestManager_IsRunning_NoSession(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Without a tmux session, IsRunning should return false
	// Note: this test doesn't create a tmux session, so it tests the "not running" case
	running, err := mgr.IsRunning()
	if err != nil {
		// If tmux server isn't running, HasSession returns an error
		// This is expected in test environments without tmux
		t.Logf("IsRunning returned error (expected without tmux): %v", err)
		return
	}

	if running {
		t.Error("IsRunning() = true, want false (no session created)")
	}
}

func TestManager_Status_NotRunning(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Without a tmux session, Status should return ErrNotRunning
	_, err := mgr.Status()
	if err == nil {
		t.Error("Status() expected error when not running")
	}
	// May return ErrNotRunning or a tmux server error
	t.Logf("Status returned error (expected): %v", err)
}

func TestManager_Queue_NoBeads(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Queue returns error when no beads database exists
	// This is expected - beads requires initialization
	_, err := mgr.Queue()
	if err == nil {
		// If beads is somehow available, queue should be empty
		t.Log("Queue() succeeded unexpectedly (beads may be available)")
		return
	}
	// Error is expected when beads isn't initialized
	t.Logf("Queue() returned error (expected without beads): %v", err)
}

func TestManager_Queue_FiltersClosedMergeRequests(t *testing.T) {
	mgr, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable in test environment: %v", err)
	}

	openIssue, err := b.Create(beads.CreateOptions{
		Title: "Open MR",
		Labels: []string{"gt:merge-request"},
	})
	if err != nil {
		t.Fatalf("create open merge-request issue: %v", err)
	}
	closedIssue, err := b.Create(beads.CreateOptions{
		Title: "Closed MR",
		Labels: []string{"gt:merge-request"},
	})
	if err != nil {
		t.Fatalf("create closed merge-request issue: %v", err)
	}
	closedStatus := "closed"
	if err := b.Update(closedIssue.ID, beads.UpdateOptions{Status: &closedStatus}); err != nil {
		t.Fatalf("close merge-request issue: %v", err)
	}

	queue, err := mgr.Queue()
	if err != nil {
		t.Fatalf("Queue() error: %v", err)
	}

	var sawOpen bool
	for _, item := range queue {
		if item.MR == nil {
			continue
		}
		if item.MR.ID == closedIssue.ID {
			t.Fatalf("queue contains closed merge-request %s", closedIssue.ID)
		}
		if item.MR.ID == openIssue.ID {
			sawOpen = true
		}
	}
	if !sawOpen {
		t.Fatalf("queue missing expected open merge-request %s", openIssue.ID)
	}
}

func TestManager_FindMR_NoBeads(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// FindMR returns error when no beads database exists
	_, err := mgr.FindMR("nonexistent-mr")
	if err == nil {
		t.Error("FindMR() expected error")
	}
	// Any error is acceptable when beads isn't initialized
	t.Logf("FindMR() returned error (expected): %v", err)
}

func TestManager_RegisterMR_Deprecated(t *testing.T) {
	mgr, _ := setupTestManager(t)

	mr := &MergeRequest{
		ID:     "gt-mr-test",
		Branch: "polecat/Test/gt-123",
		Worker: "Test",
		Status: MROpen,
	}

	// RegisterMR should return an error indicating deprecation
	err := mgr.RegisterMR(mr)
	if err == nil {
		t.Error("RegisterMR() expected error (deprecated)")
	}
}

func TestManager_Retry_Deprecated(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Retry is deprecated and should not error, just print a message
	err := mgr.Retry("any-id", false)
	if err != nil {
		t.Errorf("Retry() unexpected error: %v", err)
	}
}

func TestCompareScoredIssues_UsesDeterministicIDTieBreaker(t *testing.T) {
	t.Helper()

	first := scoredIssue{
		issue: &beads.Issue{
			ID:        "gt-1",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		score: 10,
	}
	second := scoredIssue{
		issue: &beads.Issue{
			ID:        "gt-2",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		score: 10,
	}

	if !compareScoredIssues(first, second) {
		t.Fatalf("expected gt-1 to sort before gt-2 for equal scores")
	}
	if compareScoredIssues(second, first) {
		t.Fatalf("expected gt-2 to sort after gt-1 for equal scores")
	}
}

func TestManager_PostMerge_ClosesMRAndSourceIssue(t *testing.T) {
	mgr, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable: %v", err)
	}

	// Create a source issue
	srcIssue, err := b.Create(beads.CreateOptions{
		Title: "Implement feature X",
		Labels: []string{"gt:task"},
	})
	if err != nil {
		t.Fatalf("create source issue: %v", err)
	}

	// Create an MR bead with branch and source_issue fields
	mrDesc := "branch: polecat/test/gt-xyz\nsource_issue: " + srcIssue.ID + "\nworker: test\ntarget: main"
	mrIssue, err := b.Create(beads.CreateOptions{
		Title:       "MR for feature X",
		Labels:      []string{"gt:merge-request"},
		Description: mrDesc,
	})
	if err != nil {
		t.Fatalf("create MR issue: %v", err)
	}

	// Run PostMerge
	result, err := mgr.PostMerge(mrIssue.ID)
	if err != nil {
		t.Fatalf("PostMerge() error: %v", err)
	}

	// Verify result
	if !result.MRClosed {
		t.Error("PostMerge() MRClosed = false, want true")
	}
	if !result.SourceIssueClosed {
		t.Error("PostMerge() SourceIssueClosed = false, want true")
	}
	if result.SourceIssueID != srcIssue.ID {
		t.Errorf("PostMerge() SourceIssueID = %s, want %s", result.SourceIssueID, srcIssue.ID)
	}
	if result.MR.Branch != "polecat/test/gt-xyz" {
		t.Errorf("PostMerge() MR.Branch = %s, want polecat/test/gt-xyz", result.MR.Branch)
	}
}

func TestManager_PostMerge_AlreadyClosedMR(t *testing.T) {
	mgr, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable: %v", err)
	}

	// Create and close an MR bead
	mrIssue, err := b.Create(beads.CreateOptions{
		Title:       "Already merged MR",
		Labels:      []string{"gt:merge-request"},
		Description: "branch: polecat/old/gt-old\ntarget: main",
	})
	if err != nil {
		t.Fatalf("create MR issue: %v", err)
	}
	if err := b.Close(mrIssue.ID); err != nil {
		t.Fatalf("close MR issue: %v", err)
	}

	// PostMerge should fail since MR is already closed and won't be in queue
	_, err = mgr.PostMerge(mrIssue.ID)
	if err == nil {
		t.Error("PostMerge() expected error for already-closed MR")
	}
}

func TestManager_PostMerge_NotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)

	_, err := mgr.PostMerge("nonexistent-mr-id")
	if err == nil {
		t.Error("PostMerge() expected error for nonexistent MR")
	}
}
