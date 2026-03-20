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

// --- Edge case tests for parseGtEventLine ---

func TestParseGtEventLine_MalformedJSONFields(t *testing.T) {
	// Valid JSON but timestamp is wrong type (number instead of string)
	// json.Unmarshal returns error for type mismatch, so parseGtEventLine returns nil
	line := `{"ts":12345,"source":"test","type":"create","actor":"a","visibility":"feed","payload":{"message":"hi"}}`
	event := parseGtEventLine(line)
	if event != nil {
		t.Error("wrong-type field should cause unmarshal error and return nil")
	}
}

func TestParseGtEventLine_NoVisibility(t *testing.T) {
	ge := GtEvent{
		Timestamp: time.Now().Format(time.RFC3339),
		Source:    "test",
		Type:      "create",
		Actor:     "a",
		// Visibility intentionally empty
		Payload: map[string]interface{}{"message": "no vis"},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event != nil {
		t.Error("empty visibility should be filtered out (not feed or both)")
	}
}

func TestParseGtEventLine_NullPayload(t *testing.T) {
	line := `{"ts":"` + time.Now().Format(time.RFC3339) + `","source":"test","type":"done","actor":"a","visibility":"feed","payload":null}`
	event := parseGtEventLine(line)
	if event == nil {
		t.Fatal("null payload should still parse")
	}
	if event.Message != "work done" {
		t.Errorf("Message = %q, want %q", event.Message, "work done")
	}
}

func TestParseGtEventLine_EmptyPayload(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "sling",
		Actor:      "gastown/witness",
		Visibility: "feed",
		Payload:    map[string]interface{}{},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("empty payload should still parse")
	}
	if event.Message != "work slung" {
		t.Errorf("Message = %q, want %q", event.Message, "work slung")
	}
}

func TestParseGtEventLine_NoActor(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "no actor"},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("missing actor should still parse")
	}
	if event.Actor != "" {
		t.Errorf("Actor = %q, want empty", event.Actor)
	}
	if event.Rig != "" {
		t.Errorf("Rig = %q, want empty (no actor to extract from)", event.Rig)
	}
}

func TestParseGtEventLine_NoType(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Actor:      "gastown/witness",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "no type"},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("missing type should still parse")
	}
	if event.Type != "" {
		t.Errorf("Type = %q, want empty", event.Type)
	}
}

func TestParseGtEventLine_MayorActorNoRig(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "create",
		Actor:      "mayor",
		Visibility: "feed",
		Payload:    map[string]interface{}{"message": "mayor event"},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	// mayor is a single-part actor, should not extract rig
	if event.Rig != "" {
		t.Errorf("Rig = %q, want empty (mayor has no rig)", event.Rig)
	}
}

func TestParseGtEventLine_PolecatActorRole(t *testing.T) {
	ge := GtEvent{
		Timestamp:  time.Now().Format(time.RFC3339),
		Source:     "test",
		Type:       "done",
		Actor:      "gastown/polecats/toast",
		Visibility: "feed",
		Payload:    map[string]interface{}{"bead": "gs-1"},
	}
	line, _ := json.Marshal(ge)
	event := parseGtEventLine(string(line))
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Rig != "gastown" {
		t.Errorf("Rig = %q, want %q", event.Rig, "gastown")
	}
	if event.Role != "polecat" {
		t.Errorf("Role = %q, want %q", event.Role, "polecat")
	}
}

func TestParseGtEventLine_TruncatedJSON(t *testing.T) {
	if parseGtEventLine(`{"ts":"2026-01-01T00:00:00Z","source`) != nil {
		t.Error("truncated JSON should return nil")
	}
}

func TestParseGtEventLine_ExtraFields(t *testing.T) {
	line := `{"ts":"` + time.Now().Format(time.RFC3339) + `","source":"test","type":"hook","actor":"a","visibility":"feed","payload":{"bead":"gt-1"},"extra_field":"ignored"}`
	event := parseGtEventLine(line)
	if event == nil {
		t.Fatal("extra fields should not break parsing")
	}
	if event.Type != "hook" {
		t.Errorf("Type = %q, want %q", event.Type, "hook")
	}
}

// --- Edge case tests for parseBdActivityLine ---

func TestParseBdActivityLine_MissingBeadID(t *testing.T) {
	// Symbol present but no bead ID
	line := "[14:30:05] + create · something happened"
	event := parseBdActivityLine(line)
	// May or may not match the regex depending on pattern; should not panic
	if event == nil {
		t.Skip("pattern didn't match, falls through to parseSimpleLine")
	}
}

func TestParseBdActivityLine_MissingAction(t *testing.T) {
	line := "[14:30:05] → gt-abc · just a description"
	event := parseBdActivityLine(line)
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	if event.Target != "gt-abc" {
		t.Errorf("Target = %q, want %q", event.Target, "gt-abc")
	}
}

func TestParseBdActivityLine_BadTimestamp(t *testing.T) {
	line := "[99:99:99] + gt-abc create · bad time"
	event := parseBdActivityLine(line)
	if event == nil {
		t.Fatal("expected non-nil event (bad time defaults to now)")
	}
	if time.Since(event.Time) > 5*time.Second {
		t.Error("bad timestamp should default to approximately now")
	}
}

func TestParseBdActivityLine_OnlyWhitespace(t *testing.T) {
	if parseBdActivityLine("   \t  ") != nil {
		t.Error("whitespace-only line should return nil via parseSimpleLine")
	}
}

func TestParseBdActivityLine_NoSymbolMatch(t *testing.T) {
	// Has timestamp bracket but unrecognized symbol
	line := "[10:00:00] ? gt-abc action · desc"
	event := parseBdActivityLine(line)
	// Falls through to parseSimpleLine
	if event == nil {
		t.Fatal("should fall through to parseSimpleLine")
	}
	if event.Type != "update" {
		t.Errorf("Type = %q, want %q (default from parseSimpleLine)", event.Type, "update")
	}
}

// --- Edge case tests for parseSimpleLine ---

func TestParseSimpleLine_MalformedTimestamp(t *testing.T) {
	event := parseSimpleLine("[not-a-time] some event")
	if event == nil {
		t.Fatal("expected non-nil event")
	}
	// Should default to now since timestamp parse fails
	if time.Since(event.Time) > 5*time.Second {
		t.Error("malformed timestamp should default to approximately now")
	}
}

func TestParseSimpleLine_ShortBracketLine(t *testing.T) {
	// Line starts with [ but is too short for timestamp extraction
	event := parseSimpleLine("[x]")
	if event == nil {
		t.Fatal("expected non-nil event")
	}
}

// --- Edge case tests for parseBeadContext ---

func TestParseBeadContext_Empty(t *testing.T) {
	actor, rig, role := parseBeadContext("")
	if actor != "" || rig != "" || role != "" {
		t.Errorf("empty beadID should return empty strings, got actor=%q rig=%q role=%q", actor, rig, role)
	}
}

func TestParseBeadContext_NonAgentBead(t *testing.T) {
	// Regular bead IDs like "gs-abc" parse via backward compat in ParseAgentBeadID
	// but parseBeadContext returns empty actor since role "abc" isn't a known role
	actor, _, _ := parseBeadContext("gs-abc")
	if actor != "" {
		t.Errorf("non-agent bead should return empty actor, got %q", actor)
	}
}

// --- Edge case tests for matchesFilters ---

func TestMatchesFilters_NoFilters(t *testing.T) {
	event := &Event{Time: time.Now(), Type: "create", Target: "gt-1", Message: "test"}
	if !matchesFilters(event, time.Time{}, "", "", "", nil) {
		t.Error("no filters should match everything")
	}
}

func TestMatchesFilters_SinceFilter(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)
	event := &Event{Time: time.Now(), Type: "create"}

	if !matchesFilters(event, past, "", "", "", nil) {
		t.Error("event after sinceTime should match")
	}
	if matchesFilters(event, future, "", "", "", nil) {
		t.Error("event before sinceTime should not match")
	}
}

func TestMatchesFilters_MolFilter(t *testing.T) {
	event := &Event{Time: time.Now(), Type: "create", Target: "gt-abc", Message: "updated gt-abc"}

	if !matchesFilters(event, time.Time{}, "gt-abc", "", "", nil) {
		t.Error("mol in target should match")
	}
	if matchesFilters(event, time.Time{}, "gt-xyz", "", "", nil) {
		t.Error("mol not in target or message should not match")
	}
}

func TestMatchesFilters_MolInMessage(t *testing.T) {
	event := &Event{Time: time.Now(), Type: "create", Target: "other", Message: "relates to gt-abc"}
	if !matchesFilters(event, time.Time{}, "gt-abc", "", "", nil) {
		t.Error("mol in message should match")
	}
}

func TestMatchesFilters_TypeFilter(t *testing.T) {
	event := &Event{Time: time.Now(), Type: "create"}
	if !matchesFilters(event, time.Time{}, "", "create", "", nil) {
		t.Error("matching type should pass")
	}
	if matchesFilters(event, time.Time{}, "", "delete", "", nil) {
		t.Error("non-matching type should fail")
	}
}

func TestMatchesFilters_RigFilter(t *testing.T) {
	event := &Event{Time: time.Now(), Type: "create", Rig: "gastown"}
	if !matchesFilters(event, time.Time{}, "", "", "gastown", nil) {
		t.Error("matching rig should pass")
	}
	if matchesFilters(event, time.Time{}, "", "", "other", nil) {
		t.Error("non-matching rig should fail")
	}
}

func TestMatchesFilters_ConvoyFilter(t *testing.T) {
	members := map[string]bool{"gt-1": true, "gt-2": true}

	event := &Event{Time: time.Now(), Type: "create", Target: "gt-1", Message: "test"}
	if !matchesFilters(event, time.Time{}, "", "", "", members) {
		t.Error("target in convoy should match")
	}

	event2 := &Event{Time: time.Now(), Type: "create", Target: "gt-99", Message: "mentions gt-2 here"}
	if !matchesFilters(event2, time.Time{}, "", "", "", members) {
		t.Error("convoy member in message should match")
	}

	event3 := &Event{Time: time.Now(), Type: "create", Target: "gt-99", Message: "no match"}
	if matchesFilters(event3, time.Time{}, "", "", "", members) {
		t.Error("no convoy member match should fail")
	}
}

func TestMatchesFilters_CombinedFilters(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	event := &Event{Time: time.Now(), Type: "create", Target: "gt-abc", Rig: "gastown"}

	// All filters match
	if !matchesFilters(event, past, "gt-abc", "create", "gastown", nil) {
		t.Error("all matching filters should pass")
	}
	// One filter fails
	if matchesFilters(event, past, "gt-abc", "delete", "gastown", nil) {
		t.Error("one non-matching filter should fail")
	}
}

// --- Edge case tests for buildEventMessage ---

func TestBuildEventMessage_NilPayload(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"patrol_started", "patrol started"},
		{"patrol_complete", "patrol complete"},
		{"polecat_checked", "polecat checked"},
		{"polecat_nudged", "polecat nudged"},
		{"sling", "work slung"},
		{"hook", "bead hooked"},
		{"handoff", "session handoff"},
		{"done", "work done"},
		{"mail", "mail sent"},
		{"merged", "merged"},
		{"merge_failed", "merge failed"},
		{"escalation_sent", "escalation sent"},
	}
	for _, tt := range tests {
		t.Run(tt.eventType+"_nil", func(t *testing.T) {
			msg := buildEventMessage(tt.eventType, nil)
			if msg != tt.want {
				t.Errorf("buildEventMessage(%q, nil) = %q, want %q", tt.eventType, msg, tt.want)
			}
		})
	}
}

func TestBuildEventMessage_PartialPayload(t *testing.T) {
	// escalation_sent with only target, no "to"
	msg := buildEventMessage("escalation_sent", map[string]interface{}{"target": "gt-1"})
	if msg != "escalation sent" {
		t.Errorf("partial escalation payload = %q, want %q", msg, "escalation sent")
	}

	// mail with subject but no "to"
	msg = buildEventMessage("mail", map[string]interface{}{"subject": "Help"})
	if msg != "Help" {
		t.Errorf("mail with subject only = %q, want %q", msg, "Help")
	}

	// polecat_checked with polecat but no status
	msg = buildEventMessage("polecat_checked", map[string]interface{}{"polecat": "slit"})
	if msg != "checked slit" {
		t.Errorf("polecat_checked no status = %q, want %q", msg, "checked slit")
	}

	// polecat_nudged with polecat but no reason
	msg = buildEventMessage("polecat_nudged", map[string]interface{}{"polecat": "toast"})
	if msg != "nudged toast" {
		t.Errorf("polecat_nudged no reason = %q, want %q", msg, "nudged toast")
	}
}
