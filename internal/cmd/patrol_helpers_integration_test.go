//go:build integration

package cmd

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

// --- Patrol discovery tests (findActivePatrol) ---

func requireBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd CLI not installed, skipping patrol test")
	}
}

func setupPatrolTestDB(t *testing.T) (string, *beads.Beads) {
	t.Helper()
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	tmpDir := t.TempDir()
	b := beads.NewIsolatedWithPort(tmpDir, port)
	// Use a unique prefix per test run to avoid cross-run contamination
	// in the shared Dolt database.
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	prefix := "pt" + hex.EncodeToString(buf[:])
	if err := b.Init(prefix); err != nil {
		t.Fatalf("bd init: %v", err)
	}

	// Clean up the test database after the test to avoid leaking
	// beads_pt* databases on the shared Dolt server.
	dbName := "beads_" + prefix
	t.Cleanup(func() {
		dsn := fmt.Sprintf("root:@tcp(127.0.0.1:%s)/", testutil.DoltContainerPort())
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Logf("cleanup: failed to connect to dolt server to drop %s: %v", dbName, err)
			return
		}
		defer db.Close()
		if _, err := db.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
			t.Logf("cleanup: failed to drop %s: %v", dbName, err)
		}
		// Purge dropped databases to prevent accumulation on disk
		db.Exec("CALL dolt_purge_dropped_databases()") //nolint:errcheck
	})

	return tmpDir, b
}

// createHookedPatrol creates a bead with a patrol title and hooks it.
// If withOpenChild is true, creates an open child bead to simulate an active patrol.
func createHookedPatrol(t *testing.T, b *beads.Beads, molName, assignee string, withOpenChild bool) string {
	t.Helper()
	root, err := b.Create(beads.CreateOptions{
		Title:    molName + " (wisp)",
		Priority: -1,
	})
	if err != nil {
		t.Fatalf("create patrol root: %v", err)
	}

	hooked := beads.StatusHooked
	if err := b.Update(root.ID, beads.UpdateOptions{
		Status:   &hooked,
		Assignee: &assignee,
	}); err != nil {
		t.Fatalf("hook patrol: %v", err)
	}

	if withOpenChild {
		_, err := b.Create(beads.CreateOptions{
			Title:    "inbox-check",
			Parent:   root.ID,
			Priority: -1,
		})
		if err != nil {
			t.Fatalf("create child: %v", err)
		}
	}
	return root.ID
}

func TestFindActivePatrolHooked(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	rootID := createHookedPatrol(t, b, molName, assignee, true /* withOpenChild */)

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	patrolID, _, found, findErr := findActivePatrol(cfg)
	if findErr != nil {
		t.Fatalf("findActivePatrol error: %v", findErr)
	}
	if !found {
		t.Fatal("expected to find active patrol, got not found")
	}
	if patrolID != rootID {
		t.Errorf("patrolID = %q, want %q", patrolID, rootID)
	}

	// Verify the patrol is still hooked (not closed)
	issue, err := b.Show(rootID)
	if err != nil {
		t.Fatalf("show patrol: %v", err)
	}
	if issue.Status != beads.StatusHooked {
		t.Errorf("patrol status = %q, want %q", issue.Status, beads.StatusHooked)
	}
}

func TestFindActivePatrolStale(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create a patrol with a closed child (simulates post-squash state)
	rootID := createHookedPatrol(t, b, molName, assignee, true /* with child */)

	// Close the child to make the patrol stale
	children, err := b.List(beads.ListOptions{Parent: rootID, Status: "all", Priority: -1})
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	for _, child := range children {
		if closeErr := b.ForceCloseWithReason("test cleanup", child.ID); closeErr != nil {
			t.Fatalf("close child: %v", closeErr)
		}
	}

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	_, _, found, findErr := findActivePatrol(cfg)
	if findErr != nil {
		t.Fatalf("findActivePatrol error: %v", findErr)
	}
	if found {
		t.Fatal("expected stale patrol (all children closed) to NOT be found as active")
	}

	// Verify the stale patrol was closed
	issue, err := b.Show(rootID)
	if err != nil {
		t.Fatalf("show patrol: %v", err)
	}
	if issue.Status != "closed" {
		t.Errorf("stale patrol status = %q, want %q", issue.Status, "closed")
	}
}

func TestFindActivePatrolZeroChildren(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create a patrol with NO children — simulates a freshly created wisp
	// whose steps haven't materialized yet. Should be treated as active,
	// not stale, to prevent race condition.
	rootID := createHookedPatrol(t, b, molName, assignee, false /* no children */)

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	patrolID, _, found, findErr := findActivePatrol(cfg)
	if findErr != nil {
		t.Fatalf("findActivePatrol error: %v", findErr)
	}
	if !found {
		t.Fatal("expected zero-children patrol to be treated as active (not stale)")
	}
	if patrolID != rootID {
		t.Errorf("patrolID = %q, want %q", patrolID, rootID)
	}

	// Verify it was NOT closed
	issue, err := b.Show(rootID)
	if err != nil {
		t.Fatalf("show patrol: %v", err)
	}
	if issue.Status != beads.StatusHooked {
		t.Errorf("zero-children patrol status = %q, want %q (should remain hooked)", issue.Status, beads.StatusHooked)
	}
}

func TestFindActivePatrolMultiple(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create 2 stale patrols (with closed children) and 1 active patrol (with open child)
	stale1 := createHookedPatrol(t, b, molName, assignee, true)
	stale2 := createHookedPatrol(t, b, molName, assignee, true)
	activeID := createHookedPatrol(t, b, molName, assignee, true)

	// Close children of stale patrols to make them stale
	for _, staleID := range []string{stale1, stale2} {
		children, err := b.List(beads.ListOptions{Parent: staleID, Status: "all", Priority: -1})
		if err != nil {
			t.Fatalf("list children of %s: %v", staleID, err)
		}
		for _, child := range children {
			if closeErr := b.ForceCloseWithReason("test cleanup", child.ID); closeErr != nil {
				t.Fatalf("close child: %v", closeErr)
			}
		}
	}

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	patrolID, _, found, findErr := findActivePatrol(cfg)
	if findErr != nil {
		t.Fatalf("findActivePatrol error: %v", findErr)
	}
	if !found {
		t.Fatal("expected to find active patrol")
	}
	if patrolID != activeID {
		t.Errorf("patrolID = %q, want %q (should return the active one)", patrolID, activeID)
	}

	// Verify active patrol is still hooked
	issue, err := b.Show(activeID)
	if err != nil {
		t.Fatalf("show active: %v", err)
	}
	if issue.Status != beads.StatusHooked {
		t.Errorf("active patrol status = %q, want %q", issue.Status, beads.StatusHooked)
	}

	// Stale patrol cleanup is not guaranteed when an active patrol is found —
	// findActivePatrol breaks early on active discovery to prevent N+1 Dolt queries
	// (gt-18dzn6p). Remaining stale beads are cleaned by burnPreviousPatrolWisps
	// when the patrol cycle ends. Verify stale beads are either closed or still hooked
	// (not left in an intermediate broken state).
	for _, id := range []string{stale1, stale2} {
		staleIssue, showErr := b.Show(id)
		if showErr != nil {
			t.Fatalf("show stale %s: %v", id, showErr)
		}
		if staleIssue.Status != "closed" && staleIssue.Status != beads.StatusHooked {
			t.Errorf("stale patrol %s status = %q, want closed or hooked", id, staleIssue.Status)
		}
	}
}

// TestFindActivePatrol_StaleCleanupCapped verifies that when many stale patrols
// accumulate with no active patrol, cleanup is capped at maxStalePurgePerRun per call
// to prevent overwhelming Dolt with sequential write queries (gt-18dzn6p).
func TestFindActivePatrol_StaleCleanupCapped(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create more stale patrols than maxStalePurgePerRun (currently 5)
	numStale := maxStalePurgePerRun + 3 // e.g., 8 total
	staleIDs := make([]string, numStale)
	for i := 0; i < numStale; i++ {
		id := createHookedPatrol(t, b, molName, assignee, true /* with child */)
		staleIDs[i] = id

		// Close the child to make the patrol stale
		children, err := b.List(beads.ListOptions{Parent: id, Status: "all", Priority: -1})
		if err != nil {
			t.Fatalf("list children of %s: %v", id, err)
		}
		for _, child := range children {
			if closeErr := b.ForceCloseWithReason("test cleanup", child.ID); closeErr != nil {
				t.Fatalf("close child of %s: %v", id, closeErr)
			}
		}
	}

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	_, _, found, findErr := findActivePatrol(cfg)
	if findErr != nil {
		t.Fatalf("findActivePatrol error: %v", findErr)
	}
	if found {
		t.Fatal("expected no active patrol (all stale)")
	}

	// Count how many stale patrols were actually closed
	closedCount := 0
	hookedCount := 0
	for _, id := range staleIDs {
		issue, err := b.Show(id)
		if err != nil {
			t.Fatalf("show stale %s: %v", id, err)
		}
		switch issue.Status {
		case "closed":
			closedCount++
		case beads.StatusHooked:
			hookedCount++
		default:
			t.Errorf("stale patrol %s unexpected status %q", id, issue.Status)
		}
	}

	// Cleanup must be capped: at most maxStalePurgePerRun beads closed per run
	if closedCount > maxStalePurgePerRun {
		t.Errorf("closed %d stale patrols, want at most %d (cap exceeded — Dolt DoS risk)",
			closedCount, maxStalePurgePerRun)
	}
	// But at least some cleanup must happen (the cap should not be zero)
	if closedCount == 0 {
		t.Errorf("no stale patrols were closed, expected up to %d", maxStalePurgePerRun)
	}
	// Total accounted for
	if closedCount+hookedCount != numStale {
		t.Errorf("closed=%d + hooked=%d != total=%d", closedCount, hookedCount, numStale)
	}
}

func TestBurnPreviousPatrolWisps(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create 3 hooked patrol wisps (simulating accumulated orphans)
	id1 := createHookedPatrol(t, b, molName, assignee, true)
	id2 := createHookedPatrol(t, b, molName, assignee, false)
	id3 := createHookedPatrol(t, b, molName, assignee, true)

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	burnPreviousPatrolWisps(cfg)

	// All 3 patrols should now be closed
	for _, id := range []string{id1, id2, id3} {
		issue, err := b.Show(id)
		if err != nil {
			t.Fatalf("show %s: %v", id, err)
		}
		if issue.Status != "closed" {
			t.Errorf("patrol %s status = %q, want %q after burn", id, issue.Status, "closed")
		}
	}
}

func TestBurnPreviousPatrolWisps_IgnoresOtherBeads(t *testing.T) {
	requireBd(t)
	tmpDir, b := setupPatrolTestDB(t)

	molName := "mol-test-patrol"
	assignee := "testrig/witness"

	// Create a patrol wisp (should be burned)
	patrolID := createHookedPatrol(t, b, molName, assignee, true)

	// Create a non-patrol hooked bead (should NOT be burned)
	other, err := b.Create(beads.CreateOptions{
		Title:    "some-other-work",
		Priority: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	hooked := beads.StatusHooked
	if err := b.Update(other.ID, beads.UpdateOptions{
		Status:   &hooked,
		Assignee: &assignee,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := PatrolConfig{
		PatrolMolName: molName,
		BeadsDir:      tmpDir,
		Assignee:      assignee,
		Beads:         b,
	}

	burnPreviousPatrolWisps(cfg)

	// Patrol should be closed
	issue, err := b.Show(patrolID)
	if err != nil {
		t.Fatalf("show patrol: %v", err)
	}
	if issue.Status != "closed" {
		t.Errorf("patrol status = %q, want %q", issue.Status, "closed")
	}

	// Non-patrol bead should still be hooked
	otherIssue, err := b.Show(other.ID)
	if err != nil {
		t.Fatalf("show other: %v", err)
	}
	if otherIssue.Status != beads.StatusHooked {
		t.Errorf("non-patrol bead status = %q, want %q (should not be burned)", otherIssue.Status, beads.StatusHooked)
	}
}
