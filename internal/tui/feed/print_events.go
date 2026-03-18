package feed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
)

// PrintOptions controls filtering and behavior for PrintGtEvents.
type PrintOptions struct {
	Limit  int
	Follow bool
	Since  string // duration string like "5m", "1h"
	Mol    string // molecule/issue ID prefix filter
	Type   string // event type filter
	Rig    string // rig name filter (matches event's Rig field)
	Convoy string // convoy ID filter (shows events for beads tracked by the convoy)
	Ctx    context.Context // optional: controls follow-mode lifecycle; nil uses signal.NotifyContext
}

// PrintGtEvents reads .events.jsonl and prints events to stdout.
// When opts.Follow is true, it tails the file for new events after printing
// the initial batch, polling every 200ms. Canceled via opts.Ctx or SIGINT.
func PrintGtEvents(townRoot string, opts PrintOptions) error {
	eventsPath := filepath.Join(townRoot, ".events.jsonl")
	file, err := os.Open(eventsPath)
	if err != nil {
		return fmt.Errorf("no events file found at %s: %w", eventsPath, err)
	}
	defer file.Close()

	// Parse --since into a cutoff time
	var sinceTime time.Time
	if opts.Since != "" {
		dur, err := time.ParseDuration(opts.Since)
		if err != nil {
			return fmt.Errorf("invalid --since duration %q: %w", opts.Since, err)
		}
		sinceTime = time.Now().Add(-dur)
	}

	// Resolve convoy members if --convoy is set
	var convoyMembers map[string]bool
	if opts.Convoy != "" {
		convoyMembers = resolveConvoyMembers(townRoot, opts.Convoy)
		if len(convoyMembers) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: no tracked beads found for convoy %s\n", opts.Convoy)
		}
	}

	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if event := parseGtEventLine(line); event != nil {
			if matchesFilters(event, sinceTime, opts.Mol, opts.Type, opts.Rig, convoyMembers) {
				events = append(events, *event)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading events: %w", err)
	}

	// Sort by time descending (most recent first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.After(events[j].Time)
	})

	// Apply limit
	if opts.Limit > 0 && len(events) > opts.Limit {
		events = events[:opts.Limit]
	}

	// Reverse to show oldest first (chronological)
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	if len(events) == 0 && !opts.Follow {
		fmt.Println("No events found in .events.jsonl")
		return nil
	}

	for _, event := range events {
		printEvent(event)
	}

	if !opts.Follow {
		return nil
	}

	// Tail mode: poll for new lines using a fresh scanner each tick.
	// bufio.Scanner sets an internal 'done' flag after EOF and won't retry,
	// so we must create a new scanner each poll cycle while preserving the
	// file offset (os.File tracks position across scanner instances).
	ctx := opts.Ctx
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s := bufio.NewScanner(file)
			s.Buffer(make([]byte, 1024*1024), 1024*1024)
			for s.Scan() {
				line := s.Text()
				if event := parseGtEventLine(line); event != nil {
					if matchesFilters(event, sinceTime, opts.Mol, opts.Type, opts.Rig, convoyMembers) {
						printEvent(*event)
					}
				}
			}
		}
	}
}

// matchesFilters checks whether an event passes the --since, --mol, --type, --rig, and --convoy filters.
func matchesFilters(event *Event, sinceTime time.Time, mol, eventType, rig string, convoyMembers map[string]bool) bool {
	if !sinceTime.IsZero() && event.Time.Before(sinceTime) {
		return false
	}
	if mol != "" && !strings.Contains(event.Target, mol) && !strings.Contains(event.Message, mol) {
		return false
	}
	if eventType != "" && event.Type != eventType {
		return false
	}
	if rig != "" && event.Rig != rig {
		return false
	}
	if convoyMembers != nil && !convoyMemberMatch(event, convoyMembers) {
		return false
	}
	return true
}

// convoyMemberMatch checks if an event's target bead is in the convoy member set.
func convoyMemberMatch(event *Event, members map[string]bool) bool {
	if event.Target != "" && members[event.Target] {
		return true
	}
	// Also check if any convoy member ID appears in the message
	for id := range members {
		if strings.Contains(event.Message, id) {
			return true
		}
	}
	return false
}

// resolveConvoyMembers queries bd for beads tracked by the given convoy ID.
func resolveConvoyMembers(townRoot, convoyID string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), constants.BdSubprocessTimeout)
	defer cancel()

	beadsDir := filepath.Join(townRoot, ".beads")
	cmd := exec.CommandContext(ctx, "bd", "dep", "list", convoyID, "-t", "tracks", "--json")
	cmd.Dir = beadsDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil
	}

	var deps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &deps); err != nil {
		return nil
	}

	members := make(map[string]bool, len(deps))
	for _, d := range deps {
		id := d.ID
		// Strip external:prefix:id wrapper if present
		if strings.HasPrefix(id, "external:") {
			parts := strings.SplitN(id, ":", 3)
			if len(parts) == 3 {
				id = parts[2]
			}
		}
		members[id] = true
	}
	return members
}

// printEvent formats and prints a single event line.
func printEvent(event Event) {
	symbol := typeSymbol(event.Type)
	ts := event.Time.Format("15:04:05")
	actor := event.Actor
	if actor == "" {
		actor = "system"
	}
	fmt.Printf("[%s] %s %-25s %s\n", ts, symbol, actor, event.Message)
}

// PrintSummary reads .events.jsonl and prints an aggregated summary.
// Groups events by type and actor over the time window specified by opts.Since.
func PrintSummary(townRoot string, opts PrintOptions) error {
	eventsPath := filepath.Join(townRoot, ".events.jsonl")
	file, err := os.Open(eventsPath)
	if err != nil {
		return fmt.Errorf("no events file found at %s: %w", eventsPath, err)
	}
	defer file.Close()

	// Parse --since into a cutoff time (required for summary)
	since := opts.Since
	if since == "" {
		since = "24h"
	}
	dur, err := time.ParseDuration(since)
	if err != nil {
		return fmt.Errorf("invalid --since duration %q: %w", since, err)
	}
	sinceTime := time.Now().Add(-dur)

	// Resolve convoy members if --convoy is set
	var convoyMembers map[string]bool
	if opts.Convoy != "" {
		convoyMembers = resolveConvoyMembers(townRoot, opts.Convoy)
	}

	// Collect matching events
	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if event := parseGtEventLine(scanner.Text()); event != nil {
			if matchesFilters(event, sinceTime, opts.Mol, opts.Type, opts.Rig, convoyMembers) {
				events = append(events, *event)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading events: %w", err)
	}

	if len(events) == 0 {
		fmt.Printf("No events in the last %s\n", since)
		return nil
	}

	// Aggregate by type
	typeCounts := map[string]int{}
	actorCounts := map[string]int{}
	for _, e := range events {
		typeCounts[e.Type]++
		if e.Actor != "" {
			actorCounts[e.Actor]++
		}
	}

	// Print header
	fmt.Printf("Summary (%s, %d events):\n", since, len(events))

	// Print type breakdown in a readable order
	typeOrder := []struct{ key, label string }{
		{"done", "completed"},
		{"complete", "beads closed"},
		{"merged", "merges"},
		{"merge_failed", "merge failures"},
		{"sling", "slings"},
		{"escalation_sent", "escalations"},
		{"handoff", "handoffs"},
		{"create", "created"},
		{"patrol_started", "patrols"},
		{"polecat_nudged", "nudges"},
		{"mail", "mails"},
	}

	var parts []string
	seen := map[string]bool{}
	for _, t := range typeOrder {
		if c, ok := typeCounts[t.key]; ok {
			parts = append(parts, fmt.Sprintf("%d %s", c, t.label))
			seen[t.key] = true
		}
	}
	// Append any unseen types
	for k, c := range typeCounts {
		if !seen[k] {
			parts = append(parts, fmt.Sprintf("%d %s", c, k))
		}
	}
	fmt.Printf("  %s\n", strings.Join(parts, ", "))

	// Print top actors
	if len(actorCounts) > 0 {
		type actorCount struct {
			actor string
			count int
		}
		var actors []actorCount
		for a, c := range actorCounts {
			actors = append(actors, actorCount{a, c})
		}
		sort.Slice(actors, func(i, j int) bool { return actors[i].count > actors[j].count })

		fmt.Printf("Top actors:\n")
		limit := 5
		if len(actors) < limit {
			limit = len(actors)
		}
		for _, a := range actors[:limit] {
			fmt.Printf("  %-30s %d events\n", a.actor, a.count)
		}
		if len(actors) > 5 {
			fmt.Printf("  ... and %d more\n", len(actors)-5)
		}
	}

	return nil
}

func typeSymbol(eventType string) string {
	switch eventType {
	case "patrol_started":
		return "\U0001F989" // owl
	case "patrol_complete":
		return "\U0001F989" // owl
	case "polecat_nudged":
		return "\u26A1" // lightning
	case "sling":
		return "\U0001F3AF" // target
	case "handoff":
		return "\U0001F91D" // handshake
	case "done":
		return "\u2713" // checkmark
	case "merged":
		return "\u2713"
	case "merge_failed":
		return "\u2717" // x
	case "create":
		return "+"
	case "complete":
		return "\u2713"
	case "fail":
		return "\u2717"
	case "delete":
		return "\u2298" // circled minus
	default:
		return "\u2192" // arrow
	}
}
