package polecat

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// managerProfile collects wall-clock timing for polecat manager operations.
// Enabled by GT_SLING_PROFILE=1 environment variable.
type managerProfile struct {
	enabled    bool
	start      time.Time
	phases     []mgrPhase
	current    string
	phaseStart time.Time
}

type mgrPhase struct {
	name     string
	duration time.Duration
}

func newManagerProfile() *managerProfile {
	return &managerProfile{
		enabled: os.Getenv("GT_SLING_PROFILE") == "1",
		start:   time.Now(),
	}
}

func (p *managerProfile) begin(name string) {
	if !p.enabled {
		return
	}
	if p.current != "" {
		p.phases = append(p.phases, mgrPhase{name: p.current, duration: time.Since(p.phaseStart)})
	}
	p.current = name
	p.phaseStart = time.Now()
}

func (p *managerProfile) report(label string) {
	if !p.enabled || len(p.phases) == 0 {
		return
	}
	if p.current != "" {
		p.phases = append(p.phases, mgrPhase{name: p.current, duration: time.Since(p.phaseStart)})
		p.current = ""
	}
	total := time.Since(p.start)
	fmt.Fprintf(os.Stderr, "\n  ⏱ %s (%s)\n", label, total.Round(time.Millisecond))
	for _, ph := range p.phases {
		pct := float64(ph.duration) / float64(total) * 100
		bar := strings.Repeat("█", int(pct/2))
		fmt.Fprintf(os.Stderr, "    %-28s %8s %5.1f%% %s\n",
			ph.name, ph.duration.Round(time.Millisecond), pct, bar)
	}
}
