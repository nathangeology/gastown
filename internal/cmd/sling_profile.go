package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/style"
)

// SlingProfile collects wall-clock timing for each phase of gt sling.
// Enabled by GT_SLING_PROFILE=1 environment variable.
type SlingProfile struct {
	enabled bool
	start   time.Time
	phases  []slingPhase
	current string
	phaseStart time.Time
}

type slingPhase struct {
	name     string
	duration time.Duration
}

// NewSlingProfile creates a profiler. Active only when GT_SLING_PROFILE=1.
func NewSlingProfile() *SlingProfile {
	enabled := os.Getenv("GT_SLING_PROFILE") == "1"
	return &SlingProfile{enabled: enabled, start: time.Now()}
}

// Begin starts timing a named phase. Ends any previous phase.
func (p *SlingProfile) Begin(name string) {
	if !p.enabled {
		return
	}
	p.endCurrent()
	p.current = name
	p.phaseStart = time.Now()
}

// End explicitly ends the current phase.
func (p *SlingProfile) End() {
	if !p.enabled {
		return
	}
	p.endCurrent()
}

func (p *SlingProfile) endCurrent() {
	if p.current != "" {
		p.phases = append(p.phases, slingPhase{
			name:     p.current,
			duration: time.Since(p.phaseStart),
		})
		p.current = ""
	}
}

// Report prints the timing breakdown.
func (p *SlingProfile) Report() {
	if !p.enabled || len(p.phases) == 0 {
		return
	}
	p.endCurrent()
	total := time.Since(p.start)

	fmt.Fprintf(os.Stderr, "\n%s gt sling profile (%s total)\n", style.Bold.Render("⏱"), total.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 60))

	for _, ph := range p.phases {
		pct := float64(ph.duration) / float64(total) * 100
		bar := strings.Repeat("█", int(pct/2))
		fmt.Fprintf(os.Stderr, "  %-30s %8s %5.1f%% %s\n",
			ph.name, ph.duration.Round(time.Millisecond), pct, bar)
	}

	// Show sequential vs parallelizable assessment
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 60))

	var sequential, parallelizable time.Duration
	for _, ph := range p.phases {
		if isParallelizable(ph.name) {
			parallelizable += ph.duration
		} else {
			sequential += ph.duration
		}
	}
	fmt.Fprintf(os.Stderr, "  Sequential:      %s\n", sequential.Round(time.Millisecond))
	fmt.Fprintf(os.Stderr, "  Parallelizable:  %s\n", parallelizable.Round(time.Millisecond))
}

// isParallelizable returns true for phases that could theoretically run concurrently.
func isParallelizable(name string) bool {
	switch name {
	case "auto-convoy", "formula-cook", "store-fields", "wake-rig-agents":
		return true
	default:
		return false
	}
}
