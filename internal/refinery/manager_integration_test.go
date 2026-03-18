//go:build integration

package refinery

import (
	"strconv"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

func TestManager_Queue_FiltersClosedMergeRequests(t *testing.T) {
	mgr, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable in test environment: %v", err)
	}

	openIssue, err := b.Create(beads.CreateOptions{
		Title:  "Open MR",
		Labels: []string{"gt:merge-request"},
	})
	if err != nil {
		t.Fatalf("create open merge-request issue: %v", err)
	}
	closedIssue, err := b.Create(beads.CreateOptions{
		Title:  "Closed MR",
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

	// Only the open MR should appear in the queue
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
		Title:  "Implement feature X",
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
