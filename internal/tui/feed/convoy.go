package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/steveyegge/gastown/internal/constants"
)

// convoyIDPattern validates convoy IDs.
var convoyIDPattern = regexp.MustCompile(`^hq-[a-zA-Z0-9-]+$`)

// Convoy represents a convoy's status for the dashboard
type Convoy struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	CreatedAt time.Time `json:"created_at"`
	ClosedAt  time.Time `json:"closed_at,omitempty"`

	// MQ status counts for tracked issues
	MQQueued  int `json:"mq_queued,omitempty"`  // Open MRs waiting to be processed
	MQActive  int `json:"mq_active,omitempty"`  // MRs currently being merged
	MQMerged  int `json:"mq_merged,omitempty"`  // Successfully merged MRs
	MQFailed  int `json:"mq_failed,omitempty"`  // Failed/rejected MRs
}

// ConvoyState holds all convoy data for the panel
type ConvoyState struct {
	InProgress []Convoy
	Landed     []Convoy
	LastUpdate time.Time
}

// FetchConvoys retrieves convoy status from town-level beads
func FetchConvoys(townRoot string) (*ConvoyState, error) {
	townBeads := filepath.Join(townRoot, ".beads")

	state := &ConvoyState{
		InProgress: make([]Convoy, 0),
		Landed:     make([]Convoy, 0),
		LastUpdate: time.Now(),
	}

	// Build a map of issue ID -> MR status from all rigs
	mqStatus := fetchMQStatusMap(townRoot)

	// Fetch open convoys
	openConvoys, err := listConvoys(townBeads, "open")
	if err != nil {
		// Not a fatal error - just return empty state
		return state, nil
	}

	for _, c := range openConvoys {
		// Get detailed status for each convoy
		convoy := enrichConvoy(townBeads, c)
		applyMQStatus(&convoy, townBeads, mqStatus)
		state.InProgress = append(state.InProgress, convoy)
	}

	// Fetch recently closed convoys (landed in last 24h)
	closedConvoys, err := listConvoys(townBeads, "closed")
	if err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, c := range closedConvoys {
			convoy := enrichConvoy(townBeads, c)
			applyMQStatus(&convoy, townBeads, mqStatus)
			if !convoy.ClosedAt.IsZero() && convoy.ClosedAt.After(cutoff) {
				state.Landed = append(state.Landed, convoy)
			}
		}
	}

	// Sort: in-progress by created (oldest first), landed by closed (newest first)
	sort.Slice(state.InProgress, func(i, j int) bool {
		return state.InProgress[i].CreatedAt.Before(state.InProgress[j].CreatedAt)
	})
	sort.Slice(state.Landed, func(i, j int) bool {
		return state.Landed[i].ClosedAt.After(state.Landed[j].ClosedAt)
	})

	return state, nil
}

// listConvoys returns convoys with the given status
func listConvoys(beadsDir, status string) ([]convoyListItem, error) {
	listArgs := []string{"list", "--type=convoy", "--status=" + status, "--json"}

	ctx, cancel := context.WithTimeout(context.Background(), constants.BdSubprocessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bd", listArgs...) //nolint:gosec // G204: args are constructed internally
	cmd.Dir = beadsDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	var items []convoyListItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		return nil, err
	}

	return items, nil
}

type convoyListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

// enrichConvoy adds tracked issue counts to a convoy
func enrichConvoy(beadsDir string, item convoyListItem) Convoy {
	convoy := Convoy{
		ID:     item.ID,
		Title:  item.Title,
		Status: item.Status,
	}

	// Parse timestamps
	if t, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
		convoy.CreatedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04", item.CreatedAt); err == nil {
		convoy.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, item.ClosedAt); err == nil {
		convoy.ClosedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04", item.ClosedAt); err == nil {
		convoy.ClosedAt = t
	}

	// Get tracked issues and their status
	tracked := getTrackedIssueStatus(beadsDir, item.ID)
	convoy.Total = len(tracked)
	for _, t := range tracked {
		if t.Status == "closed" {
			convoy.Completed++
		}
	}

	return convoy
}

// mrStatusEntry holds the status of a single MR bead.
type mrStatusEntry struct {
	SourceIssue string
	Status      string // open, in_progress, closed
	CloseReason string // merged, rejected, conflict, superseded
}

// fetchMQStatusMap queries all rigs for MR beads and returns a map of
// source_issue ID -> MR status. This enables the convoy panel to show
// which tracked issues have MRs in the merge queue.
func fetchMQStatusMap(townRoot string) map[string]mrStatusEntry {
	result := make(map[string]mrStatusEntry)

	// Discover rig directories
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' || entry.Name() == "mayor" || entry.Name() == "daemon" || entry.Name() == "deacon" || entry.Name() == "docs" {
			continue
		}

		// Check for beads directory in the rig's refinery/rig path (where MRs live)
		rigBeads := filepath.Join(townRoot, entry.Name(), "refinery", "rig", ".beads")
		if _, err := os.Stat(rigBeads); err != nil {
			// Try direct rig path
			rigBeads = filepath.Join(townRoot, entry.Name(), ".beads")
			if _, err := os.Stat(rigBeads); err != nil {
				continue
			}
		}

		// Query MR beads from this rig
		mrs := queryMRBeads(rigBeads)
		for _, mr := range mrs {
			if mr.SourceIssue != "" {
				result[mr.SourceIssue] = mr
			}
		}
	}

	return result
}

// queryMRBeads queries merge-request beads from a beads directory.
// MR beads are stored as wisps (ephemeral), so we use bd sql to query
// the wisps table directly, matching the pattern in beads.ListMergeRequests.
func queryMRBeads(beadsDir string) []mrStatusEntry {
	ctx, cancel := context.WithTimeout(context.Background(), constants.BdSubprocessTimeout)
	defer cancel()

	query := "SELECT w.id, w.status, w.description " +
		"FROM wisps w " +
		"JOIN wisp_labels l ON w.id = l.issue_id " +
		"WHERE l.label = 'gt:merge-request'"

	cmd := exec.CommandContext(ctx, "bd", "sql", "--json", query) //nolint:gosec
	cmd.Dir = beadsDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil
	}

	var rows []struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		return nil
	}

	var entries []mrStatusEntry
	for _, row := range rows {
		entry := mrStatusEntry{Status: row.Status}
		// Parse source_issue and close_reason from description
		for _, line := range strings.Split(row.Description, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "source_issue:") {
				entry.SourceIssue = strings.TrimSpace(strings.TrimPrefix(line, "source_issue:"))
			} else if strings.HasPrefix(line, "close_reason:") {
				entry.CloseReason = strings.TrimSpace(strings.TrimPrefix(line, "close_reason:"))
			}
		}
		entries = append(entries, entry)
	}

	return entries
}

// applyMQStatus sets MQ counts on a convoy by matching tracked issues against the MR status map.
func applyMQStatus(convoy *Convoy, beadsDir string, mqStatus map[string]mrStatusEntry) {
	if len(mqStatus) == 0 {
		return
	}

	tracked := getTrackedIssueStatus(beadsDir, convoy.ID)
	for _, t := range tracked {
		mr, ok := mqStatus[t.ID]
		if !ok {
			continue
		}
		switch mr.Status {
		case "open":
			convoy.MQQueued++
		case "in_progress":
			convoy.MQActive++
		case "closed":
			if mr.CloseReason == "merged" {
				convoy.MQMerged++
			} else {
				convoy.MQFailed++
			}
		}
	}
}

// Convoy panel styles
var (
	ConvoyPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDim).
				Padding(0, 1)

	ConvoyTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	ConvoySectionStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Bold(true)

	ConvoyIDStyle = lipgloss.NewStyle().
			Foreground(colorHighlight)

	ConvoyNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

	ConvoyProgressStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	ConvoyLandedStyle = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	ConvoyAgeStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	ConvoyMQStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("33")) // blue for MQ status

	ConvoyMQActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")) // orange for active merge
)

// renderConvoyPanel renders the convoy status panel
func (m *Model) renderConvoyPanel() string {
	style := ConvoyPanelStyle
	if m.focusedPanel == PanelConvoy {
		style = FocusedBorderStyle
	}
	// Add title before content
	title := ConvoyTitleStyle.Render("🚚 Convoys")
	content := title + "\n" + m.convoyViewport.View()
	return style.Width(m.width - 2).Render(content)
}

// renderConvoys renders the convoy panel content
// renderConvoys renders the convoy status content.
// Caller must hold m.mu.
func (m *Model) renderConvoys() string {
	if m.convoyState == nil {
		return AgentIdleStyle.Render("Loading convoys...")
	}

	var lines []string

	// In Progress section
	lines = append(lines, ConvoySectionStyle.Render("IN PROGRESS"))
	if len(m.convoyState.InProgress) == 0 {
		lines = append(lines, "  "+AgentIdleStyle.Render("No active convoys"))
	} else {
		for _, c := range m.convoyState.InProgress {
			lines = append(lines, renderConvoyLine(c, false))
		}
	}

	lines = append(lines, "")

	// Recently Landed section
	lines = append(lines, ConvoySectionStyle.Render("RECENTLY LANDED (24h)"))
	if len(m.convoyState.Landed) == 0 {
		lines = append(lines, "  "+AgentIdleStyle.Render("No recent landings"))
	} else {
		for _, c := range m.convoyState.Landed {
			lines = append(lines, renderConvoyLine(c, true))
		}
	}

	return strings.Join(lines, "\n")
}

// renderConvoyLine renders a single convoy status line
func renderConvoyLine(c Convoy, landed bool) string {
	// Format: "  hq-xyz  Title       2/4 ●●○○  MQ: 1⚙ 1✓" or "  hq-xyz  Title       ✓ 2h ago"
	id := ConvoyIDStyle.Render(c.ID)

	// Truncate title if too long (rune-safe to avoid splitting multi-byte UTF-8)
	title := c.Title
	if utf8.RuneCountInString(title) > 20 {
		runes := []rune(title)
		title = string(runes[:17]) + "..."
	}
	title = ConvoyNameStyle.Render(title)

	if landed {
		// Show checkmark and time since landing
		age := formatAge(time.Since(c.ClosedAt))
		status := ConvoyLandedStyle.Render("✓") + " " + ConvoyAgeStyle.Render(age+" ago")
		return fmt.Sprintf("  %s  %-20s  %s", id, title, status)
	}

	// Show progress bar
	progress := renderProgressBar(c.Completed, c.Total)
	count := ConvoyProgressStyle.Render(fmt.Sprintf("%d/%d", c.Completed, c.Total))

	// Show MQ status if any MRs exist
	mqInfo := renderMQStatus(c)

	if mqInfo != "" {
		return fmt.Sprintf("  %s  %-20s  %s %s  %s", id, title, count, progress, mqInfo)
	}
	return fmt.Sprintf("  %s  %-20s  %s %s", id, title, count, progress)
}

// renderProgressBar creates a simple progress bar: ●●○○
func renderProgressBar(completed, total int) string {
	if total == 0 {
		return ""
	}

	// Cap at 5 dots for display
	displayTotal := total
	if displayTotal > 5 {
		displayTotal = 5
	}

	filled := (completed * displayTotal) / total
	if filled > displayTotal {
		filled = displayTotal
	}

	bar := strings.Repeat("●", filled) + strings.Repeat("○", displayTotal-filled)
	return ConvoyProgressStyle.Render(bar)
}

// renderMQStatus renders a compact MQ status string for a convoy.
// Shows counts of MRs in various states: ⚙ queued, ⏳ active, ✓ merged, ✗ failed.
func renderMQStatus(c Convoy) string {
	total := c.MQQueued + c.MQActive + c.MQMerged + c.MQFailed
	if total == 0 {
		return ""
	}

	var parts []string
	if c.MQActive > 0 {
		parts = append(parts, ConvoyMQActiveStyle.Render(fmt.Sprintf("⚙%d", c.MQActive)))
	}
	if c.MQQueued > 0 {
		parts = append(parts, ConvoyMQStyle.Render(fmt.Sprintf("⏳%d", c.MQQueued)))
	}
	if c.MQMerged > 0 {
		parts = append(parts, ConvoyLandedStyle.Render(fmt.Sprintf("✓%d", c.MQMerged)))
	}
	if c.MQFailed > 0 {
		parts = append(parts, EventFailStyle.Render(fmt.Sprintf("✗%d", c.MQFailed)))
	}

	return ConvoyMQStyle.Render("MQ:") + " " + strings.Join(parts, " ")
}
