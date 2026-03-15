package feed

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(output)
}

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	got = normalizeGolden(got)
	expected := normalizeGolden(string(want))
	if got != expected {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, expected)
	}
}

func normalizeGolden(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func writeSyntheticEvents(t *testing.T, dir string, events []GtEvent) {
	t.Helper()

	lines := make([]string, 0, len(events))
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		lines = append(lines, string(data))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".events.jsonl"), []byte(content), 0644); err != nil {
		t.Fatalf("write synthetic stream: %v", err)
	}
}

func TestPrintGtEvents_PlainGolden(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticEvents(t, dir, []GtEvent{
		{
			Timestamp:  "2026-03-15T12:00:00Z",
			Source:     "test",
			Type:       "create",
			Actor:      "gastown/witness",
			Visibility: "feed",
			Payload:    map[string]interface{}{"message": "seeded workspace"},
		},
		{
			Timestamp:  "2026-03-15T12:01:00Z",
			Source:     "test",
			Type:       "sling",
			Actor:      "gastown/crew/ember",
			Visibility: "feed",
			Payload:    map[string]interface{}{"bead": "gs-9k0", "target": "polecat-1"},
		},
		{
			Timestamp:  "2026-03-15T12:02:00Z",
			Source:     "test",
			Type:       "mail",
			Actor:      "mayor",
			Visibility: "feed",
			Payload:    map[string]interface{}{"subject": "Need eyes", "to": "gastown/witness"},
		},
	})

	output := captureStdout(t, func() {
		if err := PrintGtEvents(dir, PrintOptions{Limit: 10}); err != nil {
			t.Fatalf("PrintGtEvents: %v", err)
		}
	})

	compareGolden(t, "print_plain.golden", output)
}

func TestGtEventsSource_SyntheticStream(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticEvents(t, dir, []GtEvent{
		{
			Timestamp:  "2026-03-15T12:00:00Z",
			Source:     "test",
			Type:       "create",
			Actor:      "gastown/witness",
			Visibility: "feed",
			Payload:    map[string]interface{}{"message": "seeded workspace"},
		},
		{
			Timestamp:  "2026-03-15T12:01:00Z",
			Source:     "test",
			Type:       "update",
			Actor:      "gastown/crew/ember",
			Visibility: "internal",
			Payload:    map[string]interface{}{"message": "hidden update"},
		},
	})

	source, err := NewGtEventsSource(dir)
	if err != nil {
		t.Fatalf("NewGtEventsSource: %v", err)
	}
	defer source.Close()

	first := expectEvent(t, source.Events(), time.Second)
	if first.Message != "seeded workspace" {
		t.Fatalf("first event message = %q, want seeded workspace", first.Message)
	}

	appended, err := json.Marshal(GtEvent{
		Timestamp:  "2026-03-15T12:02:00Z",
		Source:     "test",
		Type:       "sling",
		Actor:      "gastown/crew/ember",
		Visibility: "feed",
		Payload:    map[string]interface{}{"bead": "gs-9k0", "target": "polecat-1"},
	})
	if err != nil {
		t.Fatalf("marshal appended event: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".events.jsonl"), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	if _, err := f.Write(append(appended, '\n')); err != nil {
		t.Fatalf("append event: %v", err)
	}
	_ = f.Close()

	second := expectEvent(t, source.Events(), 2*time.Second)
	if second.Type != "sling" {
		t.Fatalf("second event type = %q, want sling", second.Type)
	}
	if second.Message != "slung gs-9k0 to polecat-1" {
		t.Fatalf("second event message = %q", second.Message)
	}
}

func expectEvent(t *testing.T, ch <-chan Event, timeout time.Duration) Event {
	t.Helper()

	select {
	case event := <-ch:
		return event
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestProblemsView_Golden(t *testing.T) {
	m := NewModel(nil)
	m.mu.Lock()
	m.width = 80
	m.height = 22
	m.viewMode = ViewProblems
	m.focusedPanel = PanelProblems
	m.problemAgents = []*ProblemAgent{
		{
			Name:          "rictus",
			SessionID:     "gt-rictus",
			Role:          "polecat",
			Rig:           "gastown",
			State:         StateGUPPViolation,
			IdleMinutes:   45,
			CurrentBeadID: "gs-9k0",
		},
		{
			Name:          "ember",
			SessionID:     "gt-ember",
			Role:          "crew",
			Rig:           "gastown",
			State:         StateWorking,
			IdleMinutes:   4,
			CurrentBeadID: "gs-aux",
		},
		{
			Name:        "witness",
			SessionID:   "gt-witness",
			Role:        "witness",
			Rig:         "gastown",
			State:       StateIdle,
			IdleMinutes: 2,
		},
	}
	m.selectedProblem = 0
	m.selectedBeadID = "gs-9k0"
	m.mu.Unlock()

	m.updateViewportSizes()
	got := stripANSI(m.View())
	compareGolden(t, "problems_view.golden", got)
}

func TestToggleProblemsView_StateTransitions(t *testing.T) {
	m := NewModel(nil)

	model, cmd := m.toggleProblemsView()
	if model != m {
		t.Fatal("toggleProblemsView should return the same model")
	}
	if m.viewMode != ViewProblems {
		t.Fatalf("viewMode = %v, want ViewProblems", m.viewMode)
	}
	if m.focusedPanel != PanelProblems {
		t.Fatalf("focusedPanel = %v, want PanelProblems", m.focusedPanel)
	}
	if cmd == nil {
		t.Fatal("expected fetch command when entering problems view without recent data")
	}

	m.lastProblemsCheck = time.Now()
	_, cmd = m.toggleProblemsView()
	if m.viewMode != ViewActivity {
		t.Fatalf("viewMode = %v, want ViewActivity", m.viewMode)
	}
	if m.focusedPanel != PanelTree {
		t.Fatalf("focusedPanel = %v, want PanelTree", m.focusedPanel)
	}
	if cmd != nil {
		t.Fatal("expected no command when leaving problems view")
	}

	_, cmd = m.toggleProblemsView()
	if cmd != nil {
		t.Fatal("expected no fetch command when problems data is still fresh")
	}
}

func TestHandleKey_ToggleProblemsView(t *testing.T) {
	m := NewModel(nil)

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.viewMode != ViewProblems {
		t.Fatalf("viewMode = %v, want ViewProblems", m.viewMode)
	}
	if cmd == nil {
		t.Fatal("expected toggle key to schedule problems fetch")
	}
}
