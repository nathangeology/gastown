package refinery

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// TestOrchestration_SlingPolecatDoneRefineryMergeBeadClosed is an end-to-end
// test that verifies the full Gas Town orchestration pipeline:
//
//  1. Sling: A bead (work item) exists and is assigned to a polecat
//  2. Polecat works: Creates a feature branch, commits changes
//  3. gt done: Pushes branch (in shared bare repo, branch is immediately visible)
//  4. Refinery merges: Engineer processes the MR, merges to main
//  5. Bead closed: Source issue and MR bead are both closed
//
// Uses a shared bare repo with git worktrees, matching the real Gas Town
// architecture where .repo.git is shared between refinery and polecats.
func TestOrchestration_SlingPolecatDoneRefineryMergeBeadClosed(t *testing.T) {
	rigPath, bareRepo := setupE2ERig(t)

	// Create refinery worktree on main
	refineryDir := filepath.Join(rigPath, "refinery", "rig")
	e2eGitRun(t, rigPath, "clone", bareRepo, refineryDir)
	e2eGitConfig(t, refineryDir)

	// Create polecat worktree
	polecatDir := filepath.Join(rigPath, "polecats", "nux")
	e2eGitRun(t, rigPath, "clone", bareRepo, polecatDir)
	e2eGitConfig(t, polecatDir)

	// --- Phase 1: Sling — bead exists, assigned to polecat ---
	sourceIssueID := "gs-test-001"
	mrBeadID := "gs-mr-001"

	// --- Phase 2: Polecat works — feature branch + commit ---
	polecatBranch := "polecat/nux/gs-test-001@mk123"
	e2eGitRun(t, polecatDir, "checkout", "-b", polecatBranch, "origin/main")
	writeFile(t, polecatDir, "feature.go", "package main\n\nfunc Feature() string { return \"done\" }\n")
	e2eGitRun(t, polecatDir, "add", "feature.go")
	e2eGitRun(t, polecatDir, "commit", "-m", "feat: add feature (gs-test-001)")

	// --- Phase 3: gt done — push branch to shared origin ---
	e2eGitRun(t, polecatDir, "push", "origin", polecatBranch)

	// Fetch in refinery so it sees the new branch
	e2eGitRun(t, refineryDir, "fetch", "origin")
	// Create local tracking branch (simulates what the shared .repo.git provides)
	e2eGitRun(t, refineryDir, "branch", polecatBranch, "origin/"+polecatBranch)

	// --- Phase 4: Refinery merges ---
	engineer := newE2EEngineer(t, rigPath)

	mr := &MRInfo{
		ID:          mrBeadID,
		Branch:      polecatBranch,
		Target:      "main",
		SourceIssue: sourceIssueID,
		Worker:      "nux",
		Rig:         "testrig",
	}

	result := engineer.ProcessMRInfo(context.Background(), mr)
	if !result.Success {
		t.Fatalf("ProcessMRInfo failed: %s\nOutput:\n%s", result.Error, engineer.output.(*bytes.Buffer).String())
	}
	if result.MergeCommit == "" {
		t.Fatal("expected non-empty merge commit SHA")
	}

	// Verify merge landed on main
	e2eGitRun(t, refineryDir, "checkout", "main")
	featurePath := filepath.Join(refineryDir, "feature.go")
	if _, err := os.Stat(featurePath); os.IsNotExist(err) {
		t.Fatal("feature.go should exist on main after merge")
	}

	// --- Phase 5: HandleMRInfoSuccess closes beads ---
	engineer.HandleMRInfoSuccess(mr, result)

	outputStr := engineer.output.(*bytes.Buffer).String()

	if !strings.Contains(outputStr, mrBeadID) {
		t.Errorf("output should reference MR bead ID %s", mrBeadID)
	}
	if !strings.Contains(outputStr, "Merged") {
		t.Error("output should contain merge success message")
	}
	if !strings.Contains(outputStr, sourceIssueID) {
		t.Errorf("output should reference source issue %s", sourceIssueID)
	}

	t.Logf("Orchestration output:\n%s", outputStr)
}

// TestOrchestration_ConflictDetection verifies that the refinery correctly
// detects merge conflicts when a polecat's branch conflicts with main.
func TestOrchestration_ConflictDetection(t *testing.T) {
	rigPath, bareRepo := setupE2ERig(t)

	refineryDir := filepath.Join(rigPath, "refinery", "rig")
	e2eGitRun(t, rigPath, "clone", bareRepo, refineryDir)
	e2eGitConfig(t, refineryDir)

	// Polecat creates conflicting change
	polecatDir := filepath.Join(rigPath, "polecats", "nux")
	e2eGitRun(t, rigPath, "clone", bareRepo, polecatDir)
	e2eGitConfig(t, polecatDir)
	polecatBranch := "polecat/nux/gs-conflict"
	e2eGitRun(t, polecatDir, "checkout", "-b", polecatBranch, "origin/main")
	writeFile(t, polecatDir, "shared.go", "package main\n\nvar X = 2 // polecat\n")
	e2eGitRun(t, polecatDir, "add", "shared.go")
	e2eGitRun(t, polecatDir, "commit", "-m", "feat: polecat change")
	e2eGitRun(t, polecatDir, "push", "origin", polecatBranch)

	// Main advances with conflicting change via a separate clone
	advanceDir := filepath.Join(rigPath, "advance")
	e2eGitRun(t, rigPath, "clone", bareRepo, advanceDir)
	e2eGitConfig(t, advanceDir)
	writeFile(t, advanceDir, "shared.go", "package main\n\nvar X = 3 // main\n")
	e2eGitRun(t, advanceDir, "add", "shared.go")
	e2eGitRun(t, advanceDir, "commit", "-m", "feat: main change")
	e2eGitRun(t, advanceDir, "push", "origin", "HEAD:refs/heads/main")

	// Fetch in refinery and create local tracking branch
	e2eGitRun(t, refineryDir, "fetch", "origin")
	e2eGitRun(t, refineryDir, "branch", polecatBranch, "origin/"+polecatBranch)

	engineer := newE2EEngineer(t, rigPath)
	mr := &MRInfo{
		ID:     "gs-mr-conflict",
		Branch: polecatBranch,
		Target: "main",
		Worker: "nux",
	}

	result := engineer.ProcessMRInfo(context.Background(), mr)
	if result.Success {
		t.Fatal("expected merge to fail due to conflict")
	}
	if !result.Conflict {
		t.Errorf("expected Conflict=true, got false. Error: %s", result.Error)
	}
}

// TestOrchestration_PreVerifiedFastPath verifies that when a polecat pre-verifies
// its branch (rebased + gates passed), the refinery skips gates.
func TestOrchestration_PreVerifiedFastPath(t *testing.T) {
	rigPath, bareRepo := setupE2ERig(t)

	refineryDir := filepath.Join(rigPath, "refinery", "rig")
	e2eGitRun(t, rigPath, "clone", bareRepo, refineryDir)
	e2eGitConfig(t, refineryDir)

	// Get main HEAD SHA for pre-verification
	refineryGit := gitpkg.NewGit(refineryDir)
	mainSHA, err := refineryGit.Rev("HEAD")
	if err != nil {
		t.Fatalf("Rev HEAD: %v", err)
	}

	// Polecat creates branch, commits, pushes
	polecatDir := filepath.Join(rigPath, "polecats", "nux")
	e2eGitRun(t, rigPath, "clone", bareRepo, polecatDir)
	e2eGitConfig(t, polecatDir)
	polecatBranch := "polecat/nux/gs-preverified"
	e2eGitRun(t, polecatDir, "checkout", "-b", polecatBranch, "origin/main")
	writeFile(t, polecatDir, "fast.go", "package main\n")
	e2eGitRun(t, polecatDir, "add", "fast.go")
	e2eGitRun(t, polecatDir, "commit", "-m", "feat: fast path")
	e2eGitRun(t, polecatDir, "push", "origin", polecatBranch)

	e2eGitRun(t, refineryDir, "fetch", "origin")
	e2eGitRun(t, refineryDir, "branch", polecatBranch, "origin/"+polecatBranch)

	engineer := newE2EEngineer(t, rigPath)
	mr := &MRInfo{
		ID:              "gs-mr-fast",
		Branch:          polecatBranch,
		Target:          "main",
		Worker:          "nux",
		PreVerified:     true,
		PreVerifiedBase: mainSHA,
	}

	result := engineer.ProcessMRInfo(context.Background(), mr)
	if !result.Success {
		t.Fatalf("ProcessMRInfo failed: %s\nOutput:\n%s", result.Error, engineer.output.(*bytes.Buffer).String())
	}

	outputStr := engineer.output.(*bytes.Buffer).String()
	if !strings.Contains(outputStr, "fast-path") {
		t.Errorf("expected fast-path message in output, got:\n%s", outputStr)
	}
}

// TestOrchestration_BranchNotFound verifies graceful handling when the
// polecat's branch has been cleaned up before the refinery processes it.
func TestOrchestration_BranchNotFound(t *testing.T) {
	rigPath, _ := setupE2ERig(t)

	refineryDir := filepath.Join(rigPath, "refinery", "rig")
	e2eGitRun(t, rigPath, "clone", filepath.Join(rigPath, ".repo.git"), refineryDir)
	e2eGitConfig(t, refineryDir)

	engineer := newE2EEngineer(t, rigPath)
	mr := &MRInfo{
		ID:     "gs-mr-ghost",
		Branch: "polecat/nux/gs-ghost-branch",
		Target: "main",
		Worker: "nux",
	}

	result := engineer.ProcessMRInfo(context.Background(), mr)
	if result.Success {
		t.Fatal("expected failure for non-existent branch")
	}
	if !result.BranchNotFound {
		t.Errorf("expected BranchNotFound=true, got false. Error: %s", result.Error)
	}
}

// --- e2e test helpers (prefixed to avoid collision with batch_test.go) ---

// setupE2ERig creates a bare repo with an initial commit on main,
// returning the rig path and bare repo path.
func setupE2ERig(t *testing.T) (rigPath, bareRepo string) {
	t.Helper()
	rigPath = t.TempDir()
	bareRepo = filepath.Join(rigPath, ".repo.git")

	e2eGitRun(t, rigPath, "init", "--bare", bareRepo)

	// Bootstrap main via temp clone
	bootstrapDir := filepath.Join(rigPath, "bootstrap")
	e2eGitRun(t, rigPath, "clone", bareRepo, bootstrapDir)
	e2eGitConfig(t, bootstrapDir)
	writeFile(t, bootstrapDir, "README.md", "# Test Repo\n")
	e2eGitRun(t, bootstrapDir, "add", "README.md")
	writeFile(t, bootstrapDir, "shared.go", "package main\n\nvar X = 1\n")
	e2eGitRun(t, bootstrapDir, "add", "shared.go")
	e2eGitRun(t, bootstrapDir, "commit", "-m", "initial commit")
	e2eGitRun(t, bootstrapDir, "push", "origin", "HEAD:refs/heads/main")
	os.RemoveAll(bootstrapDir)

	// Create required rig directories
	os.MkdirAll(filepath.Join(rigPath, "refinery"), 0755)
	os.MkdirAll(filepath.Join(rigPath, "polecats"), 0755)

	return rigPath, bareRepo
}

func newE2EEngineer(t *testing.T, rigPath string) *Engineer {
	t.Helper()
	r := &rig.Rig{Name: "testrig", Path: rigPath}
	e := NewEngineer(r)
	e.output = &bytes.Buffer{}
	e.mergeSlotEnsureExists = func() (string, error) { return "test-slot", nil }
	e.mergeSlotAcquire = func(holder string, addWaiter bool) (*beads.MergeSlotStatus, error) {
		return &beads.MergeSlotStatus{Available: true, Holder: holder}, nil
	}
	e.mergeSlotRelease = func(holder string) error { return nil }
	return e
}

func e2eGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func e2eGitConfig(t *testing.T, dir string) {
	t.Helper()
	e2eGitRun(t, dir, "config", "user.email", "test@test.com")
	e2eGitRun(t, dir, "config", "user.name", "Test")
}
