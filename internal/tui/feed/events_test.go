package feed

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseGtEventLine_Valid(t *testing.T) {
	now := time.Now()
	ge := GtEvent{
		Timestamp:  now.Format(time.RFC3339),
		Source:     "test",
		Type:       "sling",
		Actor:      "gastown/crew/joe",
		Visibility: "feed",
		Payload:    map[string]interface{}{"bead": "gt-abc", "target": "polecat-1"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != "sling" {
		t.Errorf("Type = %q, want %q", event.Type, "sling")
	}
	if event.Actor != "gastown/crew/joe" {
		t.Errorf("Actor = %q, want %q", event.Actor, "gastown/crew/joe")
	}
	if event.Target != "gt-abc" {
		t.Errorf("Target = %q, want %q", event.Target, "gt-abc")
	}
	if event.Rig != "gastown" {
		t.Errorf("Rig = %q, want %q", event.Rig, "gastown")
	}
}

func TestParseGtEventLine_InternalVisibilityFiltered(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Actor:      "a",
		Visibility: "internal",
		Payload:    map[string]interface{}{"message": "hidden"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event != nil {
		t.Error("internal visibility events should be filtered out")
	}
}

func TestParseGtEventLine_BothVisibilityAllowed(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Actor:      "a",
		Visibility: "both",
		Payload:    map[string]interface{}{"message": "visible"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event == nil {
		t.Error("'both' visibility events should be allowed")
	}
}

func TestParseGtEventLine_EmptyLine(t *testing.T) {
	if parseGtEventLine("") != nil {
		t.Error("empty line should return nil")
	}
	if parseGtEventLine("   ") != nil {
		t.Error("whitespace-only line should return nil")
	}
}

func TestParseGtEventLine_InvalidJSON(t *testing.T) {
	if parseGtEventLine("{not json}") != nil {
		t.Error("invalid JSON should return nil")
	}
}

func TestParseGtEventLine_MissingTimestamp(t *testing.T) {
	ge := GtEvent{
		Source:     "test",
		Type:       "create",
		Actor:      "a",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "no ts"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("missing timestamp should still parse (uses time.Now)")
	}
	// Time should be approximately now
	if time.Since(event.Time) > 5*time.Second {
		t.Error("missing timestamp should default to approximately now")
	}
}

func TestParseGtEventLine_RigFromPayload(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Actor:      "system",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "event", "rig": "greenplace"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Rig != "greenplace" {
		t.Errorf("Rig = %q, want %q (from payload)", event.Rig, "greenplace")
	}
}

func TestParseGtEventLine_RigFromActor(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Actor:      "bluecove/witness",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "event"},
	}
	line, _ := json.Marshal(ge)

	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Rig != "bluecove" {
		t.Errorf("Rig = %q, want %q (from actor)", event.Rig, "bluecove")
	}
}

func TestBuildEventMessage_AllTypes(t *testing.T) {
	tests := []struct {
		eventType string
		payload   map[string]interface{}
		contains  string
	}{
		{"patrol_started", map[string]interface{}{"polecat_count": float64(3)}, "3 polecats"},
		{"patrol_started", nil, "patrol started"},
		{"patrol_complete", map[string]interface{}{"message": "custom msg"}, "custom msg"},
		{"polecat_checked", map[string]interface{}{"polecat": "slit", "status": "ok"}, "checked slit (ok)"},
		{"polecat_nudged", map[string]interface{}{"polecat": "toast", "reason": "idle"}, "nudged toast: idle"},
		{"sling", map[string]interface{}{"bead": "gt-1", "target": "p1"}, "slung gt-1 to p1"},
		{"sling", nil, "work slung"},
		{"hook", map[string]interface{}{"bead": "gt-2"}, "hooked gt-2"},
		{"handoff", map[string]interface{}{"subject": "PR review"}, "handoff: PR review"},
		{"done", map[string]interface{}{"bead": "gt-3"}, "done: gt-3"},
		{"mail", map[string]interface{}{"subject": "Help", "to": "witness"}, "→ witness: Help"},
		{"merged", map[string]interface{}{"worker": "slit"}, "merged work from slit"},
		{"merge_failed", map[string]interface{}{"reason": "tests"}, "merge failed: tests"},
		{"unknown_type", map[string]interface{}{"message": "custom"}, "custom"},
		{"unknown_type", nil, "unknown_type"},
		{"escalation_sent", map[string]interface{}{"target": "gt-1", "to": "witness", "reason": "stuck"}, "escalated gt-1 to witness: stuck"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			msg := buildEventMessage(tt.eventType, tt.payload)
			if msg == "" {
				t.Error("message should not be empty")
			}
			if tt.contains != "" && !containsStr(msg, tt.contains) {
				t.Errorf("buildEventMessage(%q) = %q, want to contain %q", tt.eventType, msg, tt.contains)
			}
		})
	}
}

func TestParseBdActivityLine_ValidPattern(t *testing.T) {
	line := "[14:30:05] + gt-abc create · new issue created"
	event := parseBdActivityLine(line)
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Type != "create" {
		t.Errorf("Type = %q, want %q", event.Type, "create")
	}
	if event.Target != "gt-abc" {
		t.Errorf("Target = %q, want %q", event.Target, "gt-abc")
	}
}

func TestParseBdActivityLine_AllSymbols(t *testing.T) {
	tests := []struct {
		symbol   string
		wantType string
	}{
		{"+", "create"},
		{"→", "update"},
		{"✓", "complete"},
		{"✗", "fail"},
		{"⊘", "delete"},
		{"📌", "pin"},
	}
	for _, tt := range tests {
		line := "[10:00:00] " + tt.symbol + " gt-xyz action · desc"
		event := parseBdActivityLine(line)
		if event == nil {
			t.Errorf("symbol %s: expected non-nil event", tt.symbol)
			continue
		}
		if event.Type != tt.wantType {
			t.Errorf("symbol %s: Type = %q, want %q", tt.symbol, event.Type, tt.wantType)
		}
	}
}

func TestParseBdActivityLine_EmptyLine(t *testing.T) {
	if parseBdActivityLine("") != nil {
		t.Error("empty line should return nil")
	}
}

func TestParseBdActivityLine_SimpleLine(t *testing.T) {
	// Lines that don't match the full pattern fall through to parseSimpleLine
	event := parseBdActivityLine("some random text")
	if event == nil {
		t.Fatal("simple line should still parse")
	}
	if event.Type != "update" {
		t.Errorf("Type = %q, want %q", event.Type, "update")
	}
}

func TestParseSimpleLine_WithTimestamp(t *testing.T) {
	event := parseSimpleLine("[09:15:30] some event happened")
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Time.Hour() != 9 || event.Time.Minute() != 15 {
		t.Errorf("Time = %v, want 09:15:xx", event.Time)
	}
}

func TestParseSimpleLine_Empty(t *testing.T) {
	if parseSimpleLine("") != nil {
		t.Error("empty line should return nil")
	}
	if parseSimpleLine("   ") != nil {
		t.Error("whitespace-only line should return nil")
	}
}

func TestTypeSymbol(t *testing.T) {
	tests := []struct {
		eventType string
		wantEmpty bool
	}{
		{"patrol_started", false},
		{"sling", false},
		{"handoff", false},
		{"done", false},
		{"merged", false},
		{"merge_failed", false},
		{"create", false},
		{"complete", false},
		{"fail", false},
		{"delete", false},
		{"unknown", false}, // defaults to arrow
	}
	for _, tt := range tests {
		sym := typeSymbol(tt.eventType)
		if sym == "" && !tt.wantEmpty {
			t.Errorf("typeSymbol(%q) returned empty string", tt.eventType)
		}
	}
}

func TestGetPayloadString(t *testing.T) {
	payload := map[string]interface{}{
		"key":    "value",
		"number": float64(42),
	}
	if got := getPayloadString(payload, "key"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
	if got := getPayloadString(payload, "number"); got != "" {
		t.Errorf("non-string should return empty, got %q", got)
	}
	if got := getPayloadString(payload, "missing"); got != "" {
		t.Errorf("missing key should return empty, got %q", got)
	}
	if got := getPayloadString(nil, "key"); got != "" {
		t.Errorf("nil payload should return empty, got %q", got)
	}
}

func TestGetPayloadInt(t *testing.T) {
	payload := map[string]interface{}{
		"count":  float64(5),
		"name":   "text",
	}
	if got := getPayloadInt(payload, "count"); got != 5 {
		t.Errorf("got %d, want %d", got, 5)
	}
	if got := getPayloadInt(payload, "name"); got != 0 {
		t.Errorf("non-number should return 0, got %d", got)
	}
	if got := getPayloadInt(nil, "count"); got != 0 {
		t.Errorf("nil payload should return 0, got %d", got)
	}
}

// containsStr is a helper to avoid importing strings in test
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
