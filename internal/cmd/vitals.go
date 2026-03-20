package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/doltserver"
	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var vitalsCmd = &cobra.Command{
	Use:     "vitals",
	GroupID: GroupDiag,
	Short:   "Show unified health dashboard",
	RunE:    runVitals,
}

func init() { rootCmd.AddCommand(vitalsCmd) }

func runVitals(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	printVitalsAgents(townRoot)
	fmt.Println()
	printVitalsDoltServers(townRoot)
	fmt.Println()
	printVitalsDatabases(townRoot)
	fmt.Println()
	printVitalsBackups(townRoot)
	return nil
}

func printVitalsAgents(townRoot string) {
	fmt.Println(style.Bold.Render("Agents"))

	// Count active sessions by role
	counts := vitalsCountSessionsByRole()
	roles := []session.Role{session.RolePolecat, session.RoleCrew, session.RoleWitness, session.RoleRefinery}
	var parts []string
	for _, r := range roles {
		if n := counts[r]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, r))
		}
	}
	if len(parts) == 0 {
		fmt.Printf("  Sessions: %s\n", style.Dim.Render("none"))
	} else {
		fmt.Printf("  Sessions: %s\n", strings.Join(parts, ", "))
	}

	// Recent errors/escalations from .events.jsonl
	errs1h, errs24h, escs1h, escs24h := vitalsCountRecentEvents(townRoot)
	fmt.Printf("  Errors:   %s (1h)  %s (24h)\n",
		vitalsColorCount(errs1h), vitalsColorCount(errs24h))
	fmt.Printf("  Escalate: %s (1h)  %s (24h)\n",
		vitalsColorCount(escs1h), vitalsColorCount(escs24h))

	// Throughput: beads closed
	closed1h, closed24h := vitalsCountBeadsClosed(townRoot)
	fmt.Printf("  Closed:   %d (1h)  %d (24h)\n", closed1h, closed24h)
}

func vitalsColorCount(n int) string {
	s := strconv.Itoa(n)
	if n > 0 {
		return style.Warning.Render(s)
	}
	return s
}

// vitalsCountSessionsByRole lists tmux sessions and counts them by role.
func vitalsCountSessionsByRole() map[session.Role]int {
	counts := make(map[session.Role]int)
	listCmd := tmux.BuildCommand("list-sessions", "-F", "#{session_name}")
	out, err := listCmd.Output()
	if err != nil {
		return counts
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		id, err := session.ParseSessionName(line)
		if err != nil {
			continue
		}
		counts[id.Role]++
	}
	return counts
}

// vitalsCountRecentEvents scans .events.jsonl for errors and escalations in the last 1h and 24h.
func vitalsCountRecentEvents(townRoot string) (errs1h, errs24h, escs1h, escs24h int) {
	eventsPath := filepath.Join(townRoot, events.EventsFile)
	f, err := os.Open(eventsPath)
	if err != nil {
		return
	}
	defer f.Close()

	now := time.Now().UTC()
	cutoff1h := now.Add(-1 * time.Hour)
	cutoff24h := now.Add(-24 * time.Hour)

	// Seek near end of file for efficiency — events older than 24h are irrelevant
	if info, err := f.Stat(); err == nil && info.Size() > 256*1024 {
		f.Seek(-256*1024, 2) //nolint:errcheck // best-effort seek
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ev struct {
			Timestamp string `json:"ts"`
			Type      string `json:"type"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, ev.Timestamp)
		if err != nil || ts.Before(cutoff24h) {
			continue
		}
		switch ev.Type {
		case events.TypeSessionDeath, events.TypeMassDeath, events.TypeMergeFailed:
			errs24h++
			if ts.After(cutoff1h) {
				errs1h++
			}
		case events.TypeEscalationSent:
			escs24h++
			if ts.After(cutoff1h) {
				escs1h++
			}
		}
	}
	return
}

// vitalsCountBeadsClosed counts beads closed in the last 1h and 24h via Dolt query.
func vitalsCountBeadsClosed(townRoot string) (closed1h, closed24h int) {
	config := doltserver.DefaultConfig(townRoot)
	databases, _ := doltserver.ListDatabases(townRoot)
	now := time.Now().UTC()
	t1h := now.Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	t24h := now.Add(-24 * time.Hour).Format("2006-01-02 15:04:05")

	for _, db := range databases {
		c1, c24 := vitalsQueryClosed(config, db, t1h, t24h)
		closed1h += c1
		closed24h += c24
	}
	return
}

func vitalsQueryClosed(config *doltserver.Config, dbName, t1h, t24h string) (int, int) {
	q := fmt.Sprintf(
		"SELECT "+
			"SUM(CASE WHEN updated_at >= '%s' THEN 1 ELSE 0 END),"+
			"SUM(CASE WHEN updated_at >= '%s' THEN 1 ELSE 0 END) "+
			"FROM %s.issues WHERE status='closed'", t1h, t24h, dbName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dolt",
		"--host", "127.0.0.1", "--port", strconv.Itoa(config.Port),
		"--user", config.User, "--no-tls", "sql", "-r", "csv", "-q", q)
	cmd.Env = append(os.Environ(), "DOLT_CLI_PASSWORD="+config.Password)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	f := strings.Split(lines[1], ",")
	if len(f) < 2 {
		return 0, 0
	}
	c1, _ := strconv.Atoi(strings.TrimSpace(f[0]))
	c24, _ := strconv.Atoi(strings.TrimSpace(f[1]))
	return c1, c24
}

func printVitalsDoltServers(townRoot string) {
	fmt.Println(style.Bold.Render("Dolt Servers"))
	config := doltserver.DefaultConfig(townRoot)
	running, pid, _ := doltserver.IsRunning(townRoot)

	if running {
		m := doltserver.GetHealthMetrics(townRoot)
		fmt.Printf("  %s :%d  production  PID %d  %s  %d/%d conn  %v\n",
			style.Success.Render("●"), config.Port, pid,
			m.DiskUsageHuman, m.Connections, m.MaxConnections,
			m.QueryLatency.Round(time.Millisecond))
		for _, w := range m.Warnings {
			fmt.Printf("    %s %s\n", style.Warning.Render("!"), w)
		}
	} else {
		fmt.Printf("  %s :%d  production  %s\n",
			style.Dim.Render("○"), config.Port, style.Dim.Render("not running"))
	}

	// Zombie dolt processes (test servers not cleaned up)
	for _, z := range findVitalsZombies(config.Port) {
		fmt.Printf("  %s :%s test zombie PID %s\n", style.Warning.Render("○"), z.port, z.pid)
	}
}

type vitalsZombie struct{ pid, port string }

// findVitalsZombies finds Dolt servers not on the production port.
// Uses lsof-based port discovery instead of pgrep/ps string matching (ZFC fix: gt-fj87).
func findVitalsZombies(prodPort int) []vitalsZombie {
	listeners := doltserver.FindAllDoltListeners()
	var zombies []vitalsZombie
	for _, l := range listeners {
		if l.Port == prodPort {
			continue
		}
		zombies = append(zombies, vitalsZombie{
			pid:  strconv.Itoa(l.PID),
			port: strconv.Itoa(l.Port),
		})
	}
	return zombies
}

func printVitalsDatabases(townRoot string) {
	databases, _ := doltserver.ListDatabases(townRoot)
	orphans, _ := doltserver.FindOrphanedDatabases(townRoot)

	if len(orphans) > 0 {
		fmt.Printf("%s (%d registered, %d orphan)\n",
			style.Bold.Render("Databases"), len(databases), len(orphans))
	} else {
		fmt.Printf("%s (%d registered)\n",
			style.Bold.Render("Databases"), len(databases))
	}

	orphanSet := make(map[string]bool)
	for _, o := range orphans {
		orphanSet[o.Name] = true
	}

	fmt.Printf("  %-12s %5s  %4s  %6s  %4s\n",
		style.Dim.Render("Rig"), style.Dim.Render("Total"),
		style.Dim.Render("Open"), style.Dim.Render("Closed"), style.Dim.Render("%"))

	config := doltserver.DefaultConfig(townRoot)
	for _, db := range databases {
		if orphanSet[db] {
			continue
		}
		s := queryVitalsStats(config, db)
		if s == nil {
			fmt.Printf("  %-12s %5s  %4s  %6s  %4s\n", db, "-", "-", "-", "-")
			continue
		}
		pct := "-"
		if s.total > 0 {
			pct = fmt.Sprintf("%d%%", s.closed*100/s.total)
		}
		fmt.Printf("  %-12s %5d  %4d  %6d  %4s\n",
			db, s.total, s.open+s.inProgress, s.closed, pct)
	}
}

type vitalsStats struct{ total, open, inProgress, closed int }

func queryVitalsStats(config *doltserver.Config, dbName string) *vitalsStats {
	q := fmt.Sprintf("SELECT COUNT(*),"+
		"SUM(CASE WHEN status='open' THEN 1 ELSE 0 END),"+
		"SUM(CASE WHEN status='in_progress' THEN 1 ELSE 0 END),"+
		"SUM(CASE WHEN status='closed' THEN 1 ELSE 0 END) "+
		"FROM %s.issues", dbName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dolt",
		"--host", "127.0.0.1", "--port", strconv.Itoa(config.Port),
		"--user", config.User, "--no-tls", "sql", "-r", "csv", "-q", q)
	cmd.Env = append(os.Environ(), "DOLT_CLI_PASSWORD="+config.Password)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil
	}
	f := strings.Split(lines[1], ",")
	if len(f) < 4 {
		return nil
	}
	total, _ := strconv.Atoi(strings.TrimSpace(f[0]))
	open, _ := strconv.Atoi(strings.TrimSpace(f[1]))
	ip, _ := strconv.Atoi(strings.TrimSpace(f[2]))
	closed, _ := strconv.Atoi(strings.TrimSpace(f[3]))
	return &vitalsStats{total, open, ip, closed}
}

func printVitalsBackups(townRoot string) {
	fmt.Println(style.Bold.Render("Backups"))

	// Local Dolt backup
	backupDir := filepath.Join(townRoot, ".dolt-backup")
	if entries, err := os.ReadDir(backupDir); err == nil {
		var count int
		var latest time.Time
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			count++
			if info, err := e.Info(); err == nil && info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		if count > 0 {
			fmt.Printf("  Local:  %s  last sync %s (%d DBs)\n",
				vitalsShortHome(backupDir), latest.Format("2006-01-02 15:04"), count)
		} else {
			fmt.Printf("  Local:  %s  %s\n", vitalsShortHome(backupDir), style.Dim.Render("empty"))
		}
	} else {
		fmt.Printf("  Local:  %s\n", style.Dim.Render("not found"))
	}

	// JSONL git archive
	archiveDir := filepath.Join(townRoot, ".dolt-archive", "git")
	out, err := exec.Command("git", "-C", archiveDir, "log", "-1", "--format=%ci").Output()
	if err != nil {
		fmt.Printf("  JSONL:  %s\n", style.Dim.Render("not available"))
		return
	}
	ts := strings.TrimSpace(string(out))
	if ts == "" {
		fmt.Printf("  JSONL:  %s\n", style.Dim.Render("no commits"))
		return
	}
	// Count records across per-rig issues.jsonl
	var records int
	if dirs, err := os.ReadDir(archiveDir); err == nil {
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(archiveDir, d.Name(), "issues.jsonl")); err == nil {
				if s := strings.TrimSpace(string(data)); s != "" {
					records += len(strings.Split(s, "\n"))
				}
			}
		}
	}
	if t, err := time.Parse("2006-01-02 15:04:05 -0700", ts); err == nil {
		ts = t.Format("2006-01-02 15:04")
	}
	fmt.Printf("  JSONL:  last push %s", ts)
	if records > 0 {
		fmt.Printf(" (%s records)", vitalsFormatCount(records))
	}
	fmt.Println()
}

func vitalsFormatCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}

func vitalsShortHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + filepath.ToSlash(path[len(home):])
	}
	return path
}
