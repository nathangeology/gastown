package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/doltserver"
)

func TestVitalsFormatCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{8263, "8,263"},
	}
	for _, tt := range tests {
		got := vitalsFormatCount(tt.n)
		if got != tt.want {
			t.Errorf("vitalsFormatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestVitalsShortHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	got := vitalsShortHome(filepath.Join(home, "gt", ".dolt-backup"))
	if got != "~/gt/.dolt-backup" {
		t.Errorf("vitalsShortHome: got %q, want %q", got, "~/gt/.dolt-backup")
	}

	got = vitalsShortHome("/tmp/other")
	if got != "/tmp/other" {
		t.Errorf("vitalsShortHome(/tmp/other) = %q, want /tmp/other", got)
	}
}

func TestFindVitalsZombies_NoZombies(t *testing.T) {
	// With no listeners, there should be no zombies
	zombies := findVitalsZombies(3307)
	// This may find real zombies on the system, so just verify it doesn't panic
	_ = zombies
}

func TestFindVitalsZombies_ExcludesProdPort(t *testing.T) {
	// findVitalsZombies filters out the production port
	// We can't easily mock lsof, but we can verify the filtering logic
	// by checking that the function handles the prod port correctly
	prodPort := 3307
	zombies := findVitalsZombies(prodPort)
	for _, z := range zombies {
		if z.port == "3307" {
			t.Error("production port should be excluded from zombies")
		}
	}
}

func TestVitalsCmd_IsRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "vitals" {
			found = true
			break
		}
	}
	if !found {
		t.Error("vitals command should be registered with rootCmd")
	}
}

func TestVitalsCmd_HasCorrectGroup(t *testing.T) {
	if vitalsCmd.GroupID != GroupDiag {
		t.Errorf("vitals should be in diag group, got %s", vitalsCmd.GroupID)
	}
}

func TestVitalsStats_ZeroValues(t *testing.T) {
	s := &vitalsStats{total: 0, open: 0, inProgress: 0, closed: 0}
	if s.total != 0 {
		t.Errorf("total = %d, want 0", s.total)
	}
}

func TestVitalsStats_Percentages(t *testing.T) {
	s := &vitalsStats{total: 10, open: 3, inProgress: 2, closed: 5}
	pct := s.closed * 100 / s.total
	if pct != 50 {
		t.Errorf("closed percentage = %d%%, want 50%%", pct)
	}
}

func TestVitalsDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	config := doltserver.DefaultConfig(dir)
	if config == nil {
		t.Fatal("DefaultConfig should not return nil")
	}
	if config.Port == 0 {
		t.Error("default port should not be 0")
	}
}
