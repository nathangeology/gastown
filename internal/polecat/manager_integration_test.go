//go:build integration

package polecat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/testutil"
	"github.com/steveyegge/gastown/internal/tmux"
)

var polecatManagerIntegrationCounter atomic.Int32

func initBeadsDBWithPrefix(t *testing.T, dir, prefix string) {
	t.Helper()
	testutil.RequireDoltContainer(t)

	args := []string{"init", "--quiet", "--prefix", prefix, "--server-port", testutil.DoltContainerPort()}
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed in %s: %v\n%s", dir, err, output)
	}

	issuesPath := filepath.Join(dir, ".beads", "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(""), 0644); err != nil {
		t.Fatalf("create issues.jsonl in %s: %v", dir, err)
	}
}

func requireTmuxIntegration(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed, skipping integration test")
	}
}

func startLiveSession(t *testing.T, sessionName string) {
	t.Helper()

	tm := tmux.NewTmux()
	if err := tm.NewSessionWithCommand(sessionName, "", "sleep 60"); err != nil {
		t.Fatalf("start tmux session %s: %v", sessionName, err)
	}
	t.Cleanup(func() {
		_ = tm.KillSessionWithProcesses(sessionName)
	})
}

// TestManagerGetPrefersHookedBeadOverStaleAgentHook verifies that manager.Get
// reports the current hooked work bead when agent hook_bead is stale.
func TestManagerGetPrefersHookedBeadOverStaleAgentHook(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed, skipping integration test")
	}
	testutil.RequireDoltContainer(t)

	n := polecatManagerIntegrationCounter.Add(1)
	prefix := fmt.Sprintf("pm%d", n)

	townRoot := t.TempDir()
	rigName := "testrig"
	rigPath := filepath.Join(townRoot, rigName)
	mayorRigPath := filepath.Join(rigPath, "mayor", "rig")

	if err := os.MkdirAll(mayorRigPath, 0755); err != nil {
		t.Fatalf("mkdir mayor rig path: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, "polecats", "toast"), 0755); err != nil {
		t.Fatalf("mkdir polecat dir: %v", err)
	}

	// Rig .beads redirects to mayor/rig/.beads so NewManager resolves correctly.
	rigBeadsDir := filepath.Join(rigPath, ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigBeadsDir, "redirect"), []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	// Town routing with unique prefix for this test DB.
	townBeadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir town .beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: prefix + "-", Path: filepath.Join(rigName, "mayor", "rig")},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	initBeadsDBWithPrefix(t, mayorRigPath, prefix)

	r := &rig.Rig{Name: rigName, Path: rigPath}
	mgr := NewManager(r, git.NewGit(rigPath), nil)

	stale, err := mgr.beads.Create(beads.CreateOptions{
		Title:    "stale old issue",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create stale issue: %v", err)
	}
	current, err := mgr.beads.Create(beads.CreateOptions{
		Title:    "current hooked issue",
		Type:     "task",
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create current issue: %v", err)
	}

	assignee := mgr.assigneeID("toast")
	hooked := beads.StatusHooked
	if err := mgr.beads.Update(current.ID, beads.UpdateOptions{
		Status:   &hooked,
		Assignee: &assignee,
	}); err != nil {
		t.Fatalf("hook current issue: %v", err)
	}

	agentID := mgr.agentBeadID("toast")
	if _, err := mgr.beads.CreateOrReopenAgentBead(agentID, assignee, &beads.AgentFields{
		HookBead:   stale.ID,
		AgentState: string(beads.AgentStateWorking),
	}); err != nil {
		t.Fatalf("create agent bead with stale hook: %v", err)
	}

	p, err := mgr.Get("toast")
	if err != nil {
		t.Fatalf("mgr.Get(toast): %v", err)
	}

	if p.State != StateWorking {
		t.Fatalf("polecat state = %q, want %q", p.State, StateWorking)
	}
	if p.Issue != current.ID {
		t.Fatalf("polecat issue = %q, want hooked issue %q (stale hook %q)", p.Issue, current.ID, stale.ID)
	}
}

func TestManagerDoesNotTreatLiveSessionAsIdle(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed, skipping integration test")
	}
	requireTmuxIntegration(t)
	testutil.RequireDoltContainer(t)

	n := polecatManagerIntegrationCounter.Add(1)
	prefix := fmt.Sprintf("pm%d", n)

	townRoot := t.TempDir()
	rigName := "testrig"
	rigPath := filepath.Join(townRoot, rigName)
	mayorRigPath := filepath.Join(rigPath, "mayor", "rig")

	if err := os.MkdirAll(mayorRigPath, 0755); err != nil {
		t.Fatalf("mkdir mayor rig path: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, "polecats", "toast"), 0755); err != nil {
		t.Fatalf("mkdir polecat dir: %v", err)
	}

	rigBeadsDir := filepath.Join(rigPath, ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigBeadsDir, "redirect"), []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	townBeadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir town .beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: prefix + "-", Path: filepath.Join(rigName, "mayor", "rig")},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	initBeadsDBWithPrefix(t, mayorRigPath, prefix)

	r := &rig.Rig{Name: rigName, Path: rigPath}
	tm := tmux.NewTmux()
	mgr := NewManager(r, git.NewGit(rigPath), tm)

	agentID := mgr.agentBeadID("toast")
	assignee := mgr.assigneeID("toast")
	if _, err := mgr.beads.CreateOrReopenAgentBead(agentID, assignee, &beads.AgentFields{
		AgentState: string(beads.AgentStateIdle),
	}); err != nil {
		t.Fatalf("create idle agent bead: %v", err)
	}

	sessionName := NewSessionManager(tm, r).SessionName("toast")
	startLiveSession(t, sessionName)

	p, err := mgr.Get("toast")
	if err != nil {
		t.Fatalf("mgr.Get(toast): %v", err)
	}
	if p.State != StateWorking {
		t.Fatalf("polecat state = %q, want %q when tmux session is alive", p.State, StateWorking)
	}

	idle, err := mgr.FindIdlePolecat()
	if err != nil {
		t.Fatalf("mgr.FindIdlePolecat(): %v", err)
	}
	if idle != nil {
		t.Fatalf("FindIdlePolecat() = %q, want nil while session %s is alive", idle.Name, sessionName)
	}
}

func TestAddWithOptions_NoPrimeMDCreatedLocally(t *testing.T) {
	// This test verifies that ProvisionPrimeMDForWorktree does NOT create
	// a local .beads/PRIME.md in the worktree when there's no tracked one.
	//
	// Bug: If redirect setup fails or ProvisionPrimeMDForWorktree doesn't
	// follow redirects correctly, it may create PRIME.md locally instead
	// of at the rig-level beads location.

	root := t.TempDir()

	// Create mayor/rig directory structure
	mayorRig := filepath.Join(root, "mayor", "rig")
	if err := os.MkdirAll(mayorRig, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}

	// Create rig-level .beads directory
	rigBeads := filepath.Join(root, ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}

	// Create redirect at rig level pointing to mayor/rig/.beads
	mayorBeads := filepath.Join(mayorRig, ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig/.beads: %v", err)
	}
	rigRedirect := filepath.Join(rigBeads, "redirect")
	if err := os.WriteFile(rigRedirect, []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	// Initialize beads database so agent bead creation works.
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	bd := beads.NewIsolatedWithPort(mayorRig, port)
	if err := bd.Init("gt"); err != nil {
		t.Fatalf("bd init: %v", err)
	}

	// Initialize git repo in mayor/rig WITHOUT any .beads/PRIME.md
	cmd := exec.Command("git", "init")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// Create a dummy file and commit (NO .beads/PRIME.md)
	dummyPath := filepath.Join(mayorRig, "README.md")
	if err := os.WriteFile(dummyPath, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	mayorGit := git.NewGit(mayorRig)
	if err := mayorGit.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := mayorGit.Commit("Initial commit without PRIME.md"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// AddWithOptions needs origin/main to exist. Add self as origin and create tracking ref.
	cmd = exec.Command("git", "remote", "add", "origin", mayorRig)
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "update-ref", "refs/remotes/origin/main", "HEAD")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-ref: %v\n%s", err, out)
	}

	// Create rig pointing to root
	r := &rig.Rig{
		Name: "rig",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	// Create polecat
	polecat, err := m.AddWithOptions("TestNoLocal", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	// BUG CHECK: The worktree should NOT have a local .beads/PRIME.md
	worktreePrimeMD := filepath.Join(polecat.ClonePath, ".beads", "PRIME.md")
	if _, err := os.Stat(worktreePrimeMD); err == nil {
		t.Errorf("PRIME.md should NOT exist in worktree .beads/ (should be at rig level via redirect): %s", worktreePrimeMD)
	}

	// Verify the redirect file exists
	worktreeRedirect := filepath.Join(polecat.ClonePath, ".beads", "redirect")
	if _, err := os.Stat(worktreeRedirect); os.IsNotExist(err) {
		t.Errorf("redirect file should exist at: %s", worktreeRedirect)
	}

	// Verify PRIME.md was created at mayor/rig/.beads/ (where redirect points)
	mayorPrimeMD := filepath.Join(mayorBeads, "PRIME.md")
	if _, err := os.Stat(mayorPrimeMD); os.IsNotExist(err) {
		t.Errorf("PRIME.md should exist at mayor/rig/.beads/: %s", mayorPrimeMD)
	}
}

func TestAddWithOptions_NoFilesAddedToRepo(t *testing.T) {
	// This test verifies the invariant that polecat creation does NOT add any
	// TRACKED files to the repo's directory structure.

	root := t.TempDir()

	// Create mayor/rig directory structure
	mayorRig := filepath.Join(root, "mayor", "rig")
	if err := os.MkdirAll(mayorRig, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}

	// Create rig-level .beads directory with redirect
	rigBeads := filepath.Join(root, ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}
	mayorBeads := filepath.Join(mayorRig, ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig/.beads: %v", err)
	}
	rigRedirect := filepath.Join(rigBeads, "redirect")
	if err := os.WriteFile(rigRedirect, []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	// Initialize beads database so agent bead creation works.
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	bd := beads.NewIsolatedWithPort(mayorRig, port)
	if err := bd.Init("gt"); err != nil {
		t.Fatalf("bd init: %v", err)
	}

	// Initialize a CLEAN git repo with known files only
	cmd := exec.Command("git", "init")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	gitignorePath := filepath.Join(mayorRig, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".claude/\n.beads/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	readmePath := filepath.Join(mayorRig, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Clean Repo\n"), 0644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	srcDir := filepath.Join(mayorRig, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	mainPath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	mayorGit := git.NewGit(mayorRig)
	if err := mayorGit.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := mayorGit.Commit("Initial commit - clean repo"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	cmd = exec.Command("git", "remote", "add", "origin", mayorRig)
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "update-ref", "refs/remotes/origin/main", "HEAD")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-ref: %v\n%s", err, out)
	}

	agentsMDPath := filepath.Join(mayorRig, "AGENTS.md")
	if err := os.WriteFile(agentsMDPath, []byte("# AGENTS\n\nFallback content.\n"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	r := &rig.Rig{
		Name: "rig",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	polecat, err := m.AddWithOptions("TestClean", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	cmd = exec.Command("git", "status", "--porcelain")
	cmd.Dir = polecat.ClonePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}

	var unexpected []string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, ".beads") {
			continue
		}
		unexpected = append(unexpected, line)
	}
	if len(unexpected) > 0 {
		t.Errorf("polecat worktree should be clean after install (no files added to repo), but git status shows:\n%s", strings.Join(unexpected, "\n"))
	}
}

func TestAddWithOptions_SettingsInstalledInPolecatsDir(t *testing.T) {
	// This test verifies that polecat creation installs .claude/settings.json
	// in the SHARED polecats/ parent directory (not inside individual worktrees).

	root := t.TempDir()

	// Create mayor/rig directory structure
	mayorRig := filepath.Join(root, "mayor", "rig")
	if err := os.MkdirAll(mayorRig, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}

	// Create rig-level .beads directory with redirect
	rigBeads := filepath.Join(root, ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}
	mayorBeads := filepath.Join(mayorRig, ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig/.beads: %v", err)
	}
	rigRedirect := filepath.Join(rigBeads, "redirect")
	if err := os.WriteFile(rigRedirect, []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	// Initialize beads database so agent bead creation works.
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	bd := beads.NewIsolatedWithPort(mayorRig, port)
	if err := bd.Init("gt"); err != nil {
		t.Fatalf("bd init: %v", err)
	}

	// Initialize a git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	readmePath := filepath.Join(mayorRig, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	mayorGit := git.NewGit(mayorRig)
	if err := mayorGit.Add("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := mayorGit.Commit("Initial commit"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	cmd = exec.Command("git", "remote", "add", "origin", mayorRig)
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "update-ref", "refs/remotes/origin/main", "HEAD")
	cmd.Dir = mayorRig
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-ref: %v\n%s", err, out)
	}

	r := &rig.Rig{
		Name: "rig",
		Path: root,
	}
	m := NewManager(r, git.NewGit(root), nil)

	polecat, err := m.AddWithOptions("TestSettings", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	// Verify settings.json exists in the SHARED polecats/ parent directory
	polecatsDir := filepath.Dir(filepath.Dir(polecat.ClonePath))
	settingsPath := filepath.Join(polecatsDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Errorf("settings.json should exist at %s (shared polecats dir) for Claude Code to find hooks", settingsPath)
	}

	// Verify settings.json does NOT exist inside the worktree
	worktreeSettingsPath := filepath.Join(polecat.ClonePath, ".claude", "settings.json")
	if _, err := os.Stat(worktreeSettingsPath); err == nil {
		t.Errorf("settings.json should NOT exist inside worktree at %s (settings are now in shared polecats dir)", worktreeSettingsPath)
	}
}
