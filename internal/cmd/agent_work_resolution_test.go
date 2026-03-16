package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestResolveAgentWork_PrefersLiveAssignedWorkOverStaleHookSlot(t *testing.T) {
	townRoot := t.TempDir()
	rigDir := filepath.Join(townRoot, "gastown")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatalf("mkdir rig dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir town beads: %v", err)
	}
	routes := []byte("{\"prefix\":\"gs-\",\"path\":\"gastown\"}\n")
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), routes, 0o644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	script := `#!/bin/sh
echo "$PWD|$*" >> "$BD_LOG"
cmd=""
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) cmd="$arg"; break ;;
  esac
done
case "$cmd" in
  list)
    echo '[{"id":"gs-hxk","title":"Recovered assignment","status":"hooked","assignee":"gastown/polecats/dementus","issue_type":"task"}]'
    ;;
  show)
    last=""
    for arg in "$@"; do
      case "$arg" in
        --*) ;;
        show) ;;
        *) last="$arg" ;;
      esac
    done
    case "$last" in
      gs-gastown-polecat-dementus)
        echo '[{"id":"gs-gastown-polecat-dementus","title":"Polecat dementus","status":"open","hook_bead":"gs-6ss","agent_state":"working"}]'
        ;;
      gs-6ss)
        echo '[{"id":"gs-6ss","title":"Old assignment","status":"closed","assignee":"gastown/polecats/dementus","issue_type":"task"}]'
        ;;
      gs-hxk)
        echo '[{"id":"gs-hxk","title":"Recovered assignment","status":"hooked","assignee":"gastown/polecats/dementus","issue_type":"task"}]'
        ;;
      *)
        echo '[]'
        ;;
    esac
    ;;
  *)
    echo '[]'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_LOG", logPath)

	ctx := RoleContext{
		Role:     RolePolecat,
		Rig:      "gastown",
		Polecat:  "dementus",
		WorkDir:  rigDir,
		TownRoot: townRoot,
	}
	got := resolveAgentWork(ctx, "gastown/polecats/dementus")
	if got == nil {
		t.Fatal("expected resolved work, got nil")
	}
	if got.ID != "gs-hxk" {
		t.Fatalf("resolved work = %q, want %q", got.ID, "gs-hxk")
	}
}

func TestSelectCurrentAgentWork_PrefersAssignedIssueOverStaleLegacyHook(t *testing.T) {
	agentID := "gastown/polecats/dementus"
	stale := &beads.Issue{ID: "gs-6ss", Status: "closed", Assignee: agentID}
	live := &beads.Issue{ID: "gs-hxk", Status: beads.StatusHooked, Assignee: agentID}

	got := selectCurrentAgentWork(agentID, []*beads.Issue{live}, stale, nil)
	if got == nil {
		t.Fatal("expected live assigned issue, got nil")
	}
	if got.ID != "gs-hxk" {
		t.Fatalf("selected issue = %q, want %q", got.ID, "gs-hxk")
	}
}

func TestSelectCurrentAgentWork_RecoversFromBranchWhenBeadsStateIsStale(t *testing.T) {
	agentID := "gastown/polecats/dementus"
	stale := &beads.Issue{ID: "gs-6ss", Status: "closed", Assignee: agentID}
	branch := &beads.Issue{ID: "gs-hxk", Status: "open"}

	got := selectCurrentAgentWork(agentID, nil, stale, branch)
	if got == nil {
		t.Fatal("expected branch-recovered issue, got nil")
	}
	if got.ID != "gs-hxk" {
		t.Fatalf("selected issue = %q, want %q", got.ID, "gs-hxk")
	}
}

func TestFindBranchRecoveredWork_UsesPolecatBranchIssue(t *testing.T) {
	workDir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	addCommit := [][]string{
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "init"},
		{"git", "checkout", "-b", "main"},
		{"git", "checkout", "-b", "polecat/dementus/gs-hxk@abc123"},
	}
	for _, args := range addCommit {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	script := `#!/bin/sh
echo "$PWD|$*" >> "$BD_LOG"
cmd=""
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) cmd="$arg"; break ;;
  esac
done
case "$cmd" in
  show)
    last=""
    for arg in "$@"; do
      case "$arg" in
        --*) ;;
        show) ;;
        *) last="$arg" ;;
      esac
    done
    if [ "$last" = "gs-hxk" ]; then
      echo '[{"id":"gs-hxk","title":"Recovered work","status":"open","assignee":"","issue_type":"task"}]'
      exit 0
    fi
    echo '[{"id":"other","title":"Other","status":"closed","assignee":"","issue_type":"task"}]'
    ;;
  *)
    echo '[]'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_LOG", logPath)

	ctx := RoleContext{
		Role:    RolePolecat,
		Rig:     "gastown",
		Polecat: "dementus",
		WorkDir: workDir,
	}
	got := findBranchRecoveredWork(ctx)
	if got == nil {
		t.Fatal("expected recovered work from branch, got nil")
	}
	if got.ID != "gs-hxk" {
		t.Fatalf("recovered issue = %q, want %q", got.ID, "gs-hxk")
	}
}
