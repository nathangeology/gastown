// Package beads provides a direct SQL client for Dolt, bypassing bd subprocess overhead.
package beads

import (
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL wire protocol driver for Dolt
)

// DirectClient provides in-process SQL access to a Dolt beads database.
// It eliminates subprocess overhead (~0.5-3s per bd call) by maintaining
// a persistent connection pool via database/sql.
//
// Gate: only used when GT_DIRECT_BEADS=1 is set. Falls back to bd subprocess otherwise.
type DirectClient struct {
	db       *sql.DB
	database string // Dolt database name (e.g., "hq", "gastown")
}

// directClientPool caches DirectClient instances by DSN to avoid opening
// duplicate connection pools for the same database.
var (
	directClientMu   sync.Mutex
	directClientPool = make(map[string]*DirectClient)
)

// DirectEnabled returns true if the direct SQL path is enabled via env var.
func DirectEnabled() bool {
	return os.Getenv("GT_DIRECT_BEADS") == "1"
}

// doltMetadata holds connection info parsed from .beads/metadata.json.
type doltMetadata struct {
	Host     string `json:"dolt_server_host"`
	Port     int    `json:"dolt_server_port"`
	Database string `json:"dolt_database"`
}

// loadDoltMetadata reads connection info from a beads directory's metadata.json.
func loadDoltMetadata(beadsDir string) (*doltMetadata, error) {
	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("reading metadata.json: %w", err)
	}
	var meta doltMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata.json: %w", err)
	}
	if meta.Host == "" {
		meta.Host = "127.0.0.1"
	}
	if meta.Port == 0 {
		meta.Port = 3307
	}
	// Allow GT_DOLT_PORT / BEADS_DOLT_PORT override (test isolation)
	if portStr := os.Getenv("GT_DOLT_PORT"); portStr != "" {
		if p := parsePort(portStr); p > 0 {
			meta.Port = p
		}
	} else if portStr := os.Getenv("BEADS_DOLT_PORT"); portStr != "" {
		if p := parsePort(portStr); p > 0 {
			meta.Port = p
		}
	}
	return &meta, nil
}

func parsePort(s string) int {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err == nil && p > 0 {
		return p
	}
	return 0
}

// NewDirectClient creates or retrieves a cached DirectClient for the given beads directory.
func NewDirectClient(beadsDir string) (*DirectClient, error) {
	meta, err := loadDoltMetadata(beadsDir)
	if err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("root@tcp(%s:%d)/%s?parseTime=true&timeout=5s", meta.Host, meta.Port, meta.Database)

	directClientMu.Lock()
	defer directClientMu.Unlock()

	if c, ok := directClientPool[dsn]; ok {
		// Verify connection is still alive
		if c.db.Ping() == nil {
			return c, nil
		}
		c.db.Close()
		delete(directClientPool, dsn)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening dolt connection: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to dolt at %s:%d/%s: %w", meta.Host, meta.Port, meta.Database, err)
	}

	c := &DirectClient{db: db, database: meta.Database}
	directClientPool[dsn] = c
	return c, nil
}

// Close closes the underlying database connection.
func (d *DirectClient) Close() error {
	return d.db.Close()
}

// Show returns a single issue by ID, querying both issues and wisps tables.
func (d *DirectClient) Show(id string) (*Issue, error) {
	issue, err := d.queryIssue(id)
	if err != nil {
		return nil, err
	}
	isWisp := false
	if issue == nil {
		// Try wisps table
		issue, err = d.queryWisp(id)
		if err != nil {
			return nil, err
		}
		isWisp = true
	}
	if issue == nil {
		return nil, ErrNotFound
	}
	// Load labels from the correct table
	labelTable := "labels"
	if isWisp {
		labelTable = "wisp_labels"
	}
	labels, _ := d.queryLabelsFrom(id, labelTable)
	issue.Labels = labels
	// Load dependencies (check both tables)
	deps, _ := d.queryDependencies(id)
	if len(deps) == 0 && isWisp {
		deps, _ = d.queryWispDependencies(id)
	}
	issue.Dependencies = deps
	return issue, nil
}

// queryIssue fetches a single issue from the issues table.
func (d *DirectClient) queryIssue(id string) (*Issue, error) {
	row := d.db.QueryRow(
		`SELECT id, title, description, status, priority, issue_type, assignee,
		        created_at, created_by, updated_at, closed_at, hook_bead, agent_state,
		        acceptance_criteria
		 FROM issues WHERE id = ?`, id)
	return scanIssue(row)
}

// queryWisp fetches a single issue from the wisps table.
func (d *DirectClient) queryWisp(id string) (*Issue, error) {
	row := d.db.QueryRow(
		`SELECT id, title, description, status, priority, issue_type, assignee,
		        created_at, created_by, updated_at, closed_at, hook_bead, agent_state,
		        acceptance_criteria
		 FROM wisps WHERE id = ?`, id)
	issue, err := scanIssue(row)
	if issue != nil {
		issue.Ephemeral = true
	}
	return issue, err
}

// scanIssue scans a row into an Issue. Returns (nil, nil) if no row found.
func scanIssue(row *sql.Row) (*Issue, error) {
	var issue Issue
	var assignee, createdBy, closedAt, hookBead, agentState, acceptCriteria sql.NullString
	var createdAt, updatedAt time.Time

	err := row.Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Priority,
		&issue.Type, &assignee, &createdAt, &createdBy, &updatedAt, &closedAt,
		&hookBead, &agentState, &acceptCriteria,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning issue: %w", err)
	}

	issue.Assignee = assignee.String
	issue.CreatedBy = createdBy.String
	issue.CreatedAt = createdAt.Format(time.RFC3339)
	issue.UpdatedAt = updatedAt.Format(time.RFC3339)
	if closedAt.Valid {
		issue.ClosedAt = closedAt.String
	}
	if hookBead.Valid {
		issue.HookBead = hookBead.String
	}
	if agentState.Valid {
		issue.AgentState = agentState.String
	}
	if acceptCriteria.Valid {
		issue.AcceptanceCriteria = acceptCriteria.String
	}
	return &issue, nil
}

// queryLabelsFrom returns all labels for an issue from the specified table.
func (d *DirectClient) queryLabelsFrom(id, table string) ([]string, error) {
	rows, err := d.db.Query(fmt.Sprintf(`SELECT label FROM %s WHERE issue_id = ?`, table), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			continue
		}
		labels = append(labels, l)
	}
	return labels, nil
}

// queryDependencies returns dependency info for an issue.
func (d *DirectClient) queryDependencies(id string) ([]IssueDep, error) {
	rows, err := d.db.Query(
		`SELECT d.depends_on_id, COALESCE(i.title,''), COALESCE(i.status,''), COALESCE(i.priority,2), COALESCE(i.issue_type,''), d.type
		 FROM dependencies d
		 LEFT JOIN issues i ON d.depends_on_id = i.id
		 WHERE d.issue_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []IssueDep
	for rows.Next() {
		var dep IssueDep
		if err := rows.Scan(&dep.ID, &dep.Title, &dep.Status, &dep.Priority, &dep.Type, &dep.DependencyType); err != nil {
			continue
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// queryWispDependencies returns dependency info from the wisp_dependencies table.
func (d *DirectClient) queryWispDependencies(id string) ([]IssueDep, error) {
	rows, err := d.db.Query(
		`SELECT d.depends_on_id, COALESCE(w.title,''), COALESCE(w.status,''), COALESCE(w.priority,2), COALESCE(w.issue_type,''), d.type
		 FROM wisp_dependencies d
		 LEFT JOIN wisps w ON d.depends_on_id = w.id
		 WHERE d.issue_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []IssueDep
	for rows.Next() {
		var dep IssueDep
		if err := rows.Scan(&dep.ID, &dep.Title, &dep.Status, &dep.Priority, &dep.Type, &dep.DependencyType); err != nil {
			continue
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// List returns issues matching the given options via direct SQL.
func (d *DirectClient) List(opts ListOptions) ([]*Issue, error) {
	table := "issues"
	labelTable := "labels"
	if opts.Ephemeral {
		table = "wisps"
		labelTable = "wisp_labels"
	}

	query := fmt.Sprintf(
		`SELECT i.id, i.title, i.description, i.status, i.priority, i.issue_type, i.assignee,
		        i.created_at, i.created_by, i.updated_at, i.closed_at, i.hook_bead, i.agent_state,
		        i.acceptance_criteria
		 FROM %s i`, table)

	var conditions []string
	var args []interface{}

	if opts.Label != "" {
		query = fmt.Sprintf(
			`SELECT i.id, i.title, i.description, i.status, i.priority, i.issue_type, i.assignee,
			        i.created_at, i.created_by, i.updated_at, i.closed_at, i.hook_bead, i.agent_state,
			        i.acceptance_criteria
			 FROM %s i JOIN %s l ON i.id = l.issue_id`, table, labelTable)
		conditions = append(conditions, "l.label = ?")
		args = append(args, opts.Label)
	}
	if opts.Status != "" && opts.Status != "all" {
		conditions = append(conditions, "i.status = ?")
		args = append(args, opts.Status)
	}
	if opts.Priority >= 0 {
		conditions = append(conditions, "i.priority = ?")
		args = append(args, opts.Priority)
	}
	if opts.Parent != "" {
		// Parent filtering requires parent_issues table or similar — fall back
		return nil, fmt.Errorf("parent filter not supported in direct mode")
	}
	if opts.Assignee != "" {
		conditions = append(conditions, "i.assignee = ?")
		args = append(args, opts.Assignee)
	}
	if opts.NoAssignee {
		conditions = append(conditions, "(i.assignee IS NULL OR i.assignee = '')")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY i.priority ASC, i.updated_at DESC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		var issue Issue
		var assignee, createdBy, closedAt, hookBead, agentState, acceptCriteria sql.NullString
		var createdAt, updatedAt time.Time

		if err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Priority,
			&issue.Type, &assignee, &createdAt, &createdBy, &updatedAt, &closedAt,
			&hookBead, &agentState, &acceptCriteria,
		); err != nil {
			continue
		}
		issue.Assignee = assignee.String
		issue.CreatedBy = createdBy.String
		issue.CreatedAt = createdAt.Format(time.RFC3339)
		issue.UpdatedAt = updatedAt.Format(time.RFC3339)
		if closedAt.Valid {
			issue.ClosedAt = closedAt.String
		}
		if hookBead.Valid {
			issue.HookBead = hookBead.String
		}
		if agentState.Valid {
			issue.AgentState = agentState.String
		}
		if acceptCriteria.Valid {
			issue.AcceptanceCriteria = acceptCriteria.String
		}
		issue.Ephemeral = opts.Ephemeral
		issues = append(issues, &issue)
	}
	return issues, nil
}

// Update updates an issue's fields via direct SQL.
func (d *DirectClient) Update(id string, opts UpdateOptions) error {
	var setClauses []string
	var args []interface{}

	if opts.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *opts.Title)
	}
	if opts.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *opts.Status)
	}
	if opts.Priority != nil {
		setClauses = append(setClauses, "priority = ?")
		args = append(args, *opts.Priority)
	}
	if opts.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *opts.Description)
	}
	if opts.Assignee != nil {
		setClauses = append(setClauses, "assignee = ?")
		args = append(args, *opts.Assignee)
	}

	if len(setClauses) == 0 && len(opts.AddLabels) == 0 && len(opts.RemoveLabels) == 0 && len(opts.SetLabels) == 0 {
		return nil // Nothing to update
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		args = append(args, id)

		// Try issues table first, then wisps
		query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(setClauses, ", "))
		result, err := d.db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating issue: %w", err)
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			// Try wisps table
			query = fmt.Sprintf("UPDATE wisps SET %s WHERE id = ?", strings.Join(setClauses, ", "))
			if _, err := d.db.Exec(query, args...); err != nil {
				return fmt.Errorf("updating wisp: %w", err)
			}
		}
	}

	// Handle label operations
	if len(opts.SetLabels) > 0 {
		d.db.Exec("DELETE FROM labels WHERE issue_id = ?", id)
		for _, label := range opts.SetLabels {
			d.db.Exec("INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)", id, label)
		}
	} else {
		for _, label := range opts.AddLabels {
			d.db.Exec("INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)", id, label)
		}
		for _, label := range opts.RemoveLabels {
			d.db.Exec("DELETE FROM labels WHERE issue_id = ? AND label = ?", id, label)
		}
	}

	return nil
}

// Create creates a new issue via direct SQL.
func (d *DirectClient) Create(opts CreateOptions) (*Issue, error) {
	if IsFlagLikeTitle(opts.Title) {
		return nil, fmt.Errorf("refusing to create bead: %w (got %q)", ErrFlagTitle, opts.Title)
	}

	// Generate ID using the database prefix
	var prefix string
	row := d.db.QueryRow("SELECT value FROM config WHERE `key` = 'issue_prefix' LIMIT 1")
	if err := row.Scan(&prefix); err != nil {
		prefix = d.database
	}
	prefix = strings.TrimSuffix(prefix, "-")

	id := generateBeadID(prefix)

	issueType := "task"
	if opts.Type != "" {
		issueType = opts.Type
	}
	priority := opts.Priority
	if priority < 0 {
		priority = 2
	}
	actor := opts.Actor
	if actor == "" {
		actor = os.Getenv("BD_ACTOR")
	}

	table := "issues"
	if opts.Ephemeral {
		table = "wisps"
	}

	query := fmt.Sprintf(
		`INSERT INTO %s (id, title, description, status, priority, issue_type, created_by, assignee)
		 VALUES (?, ?, ?, 'open', ?, ?, ?, '')`, table)

	_, err := d.db.Exec(query, id, opts.Title, opts.Description, priority, issueType, actor)
	if err != nil {
		return nil, fmt.Errorf("creating issue: %w", err)
	}

	// Add labels
	labels := opts.Labels
	if len(labels) == 0 && opts.Label != "" {
		labels = []string{opts.Label}
	} else if len(labels) == 0 && opts.Type != "" {
		labels = []string{"gt:" + opts.Type}
	}

	labelTable := "labels"
	if opts.Ephemeral {
		labelTable = "wisp_labels"
	}
	for _, label := range labels {
		d.db.Exec(fmt.Sprintf("INSERT IGNORE INTO %s (issue_id, label) VALUES (?, ?)", labelTable), id, label)
	}

	// Set parent
	if opts.Parent != "" {
		d.db.Exec(
			`INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES (?, ?, 'parent', ?)`,
			id, opts.Parent, actor)
	}

	return d.Show(id)
}

// GetAgentBead retrieves an agent bead by ID via direct SQL.
func (d *DirectClient) GetAgentBead(id string) (*Issue, *AgentFields, error) {
	issue, err := d.Show(id)
	if err != nil {
		return nil, nil, err
	}
	if !IsAgentBead(issue) {
		return nil, nil, fmt.Errorf("issue %s is not an agent bead (type=%s)", id, issue.Type)
	}
	fields := ParseAgentFields(issue.Description)
	if issue.AgentState != "" {
		fields.AgentState = issue.AgentState
	}
	return issue, fields, nil
}

// GetAssignedIssue returns the first active issue assigned to the given assignee.
func (d *DirectClient) GetAssignedIssue(assignee string) (*Issue, error) {
	for _, status := range []string{"open", "in_progress", StatusHooked} {
		issues, err := d.List(ListOptions{
			Status:   status,
			Assignee: assignee,
			Priority: -1,
			Limit:    1,
		})
		if err != nil {
			return nil, err
		}
		if len(issues) > 0 {
			return issues[0], nil
		}
	}
	return nil, nil
}

// generateBeadID creates a short random bead ID with the given prefix.
func generateBeadID(prefix string) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 5)
	cryptoRand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return prefix + "-" + string(b)
}
