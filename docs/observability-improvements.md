# Observability Improvements Proposal

**Bead**: gs-pyv
**Date**: 2026-03-18
**Author**: cheedo (polecat)

## Current State

Gas Town has five observability commands: `gt feed`, `gt dashboard`, `gt status`,
`gt vitals`, and `gt convoy list/status`. Each serves a different audience and
use case, but there are gaps in coverage, filtering, and test quality.

### LOC and Test Coverage

| File | LOC | Test LOC | Ratio | Notes |
|------|-----|----------|-------|-------|
| feed.go (cmd) | 382 | 0 | 0% | No tests at all |
| dashboard.go | 187 | 150 | 80% | Good, but only flag/registration tests |
| vitals.go | 233 | 41 | 18% | Only `formatCount` and `shortHome` tested |
| statusline.go | 729 | 94 | 13% | Only `categorizeSession` tested |
| status.go | 1907 | 553 | 29% | Decent struct coverage, no runtime tests |
| tui/feed/events.go | 656 | 0* | 0% | Tested indirectly via print_events_test |
| tui/feed/print_events.go | 182 | 388 | 213% | Excellent coverage |
| tui/feed/stuck.go | 345 | 790 | 229% | Excellent coverage |
| tui/feed/model.go | 853 | 317 | 37% | Only race tests |

*events.go parsing is tested through print_events_test.go but has no direct unit tests
for `parseGtEventLine`, `buildEventMessage`, or `parseBeadContext`.

## Identified Gaps

### 1. Feed: No Convoy Filtering (`--convoy`)

`gt feed` supports `--rig` and `--mol` filters but not `--convoy`. When tracking
a convoy's progress, you must manually grep or visually scan. Adding `--convoy <id>`
to filter events by convoy membership would close this gap.

**Proposed change**: Add `--convoy` flag to `gt feed` that filters events whose
target bead belongs to the specified convoy.

### 2. Dashboard: No Port Fallback

`gt dashboard` binds to port 8080 with no fallback. If the port is in use, it
fails with a cryptic `bind: address already in use` error. The `--port` flag
exists but requires the user to know the port is taken.

**Proposed change**: Add `--port 0` support (OS-assigned port) and print the
actual port on startup. Consider auto-incrementing from 8080 on EADDRINUSE.

### 3. Status Text Truncation

`gt status --json` is comprehensive, but the text output truncates agent info
(work titles, mail subjects). Long bead titles get cut without indication.

**Proposed change**: Use terminal width detection (already imported via
`golang.org/x/term`) to dynamically size columns. Show `…` truncation indicator.

### 4. Vitals: No Agent Health Metrics

`gt vitals` shows Dolt server health, database stats, and backup status, but
nothing about agent health: uptime, error rates, throughput, or session counts.

**Proposed change**: Add an "Agents" section to vitals showing:
- Active session count by role (witness, refinery, crew, polecat)
- Recent error/escalation count (from .events.jsonl)
- Throughput: beads closed in last 1h/24h

### 5. Feed Events Lack Structured Context

Many events are `session_start`/`session_death` with no context about what work
was being done. When a polecat dies, the feed shows "session death" but not which
bead it was working on.

**Proposed change**: Enrich session lifecycle events with `hook_bead` from the
agent bead at emit time. This is a change in the event emitters (witness, sling),
not in the feed display code.

### 6. No Historical Summary

There's no easy way to answer "what happened in the last 24h?" The feed shows
raw events, but no aggregation. `gt status` is point-in-time only.

**Proposed change**: Add `gt feed --summary` that aggregates events by type and
actor over a time window. Output: "12 beads closed, 3 merges, 2 escalations,
1 merge failure" style summary.

### 7. Convoy Status Output is Sparse

`gt convoy status <id>` shows basic convoy info but doesn't show per-bead
progress, which polecats are working on which beads, or merge queue status.

**Proposed change**: Enrich convoy status with bead-level detail: status,
assignee, and last activity timestamp for each bead in the convoy.

### 8. No Unified Dashboard View

No single view combines convoy progress + polecat health + merge queue status.
The TUI feed has panels but they're separate concerns.

**Proposed change**: This is partially addressed by the existing TUI feed with
its agent tree + convoy panel + event stream. The gap is that the convoy panel
doesn't show merge queue status. Adding MQ status to the convoy panel would
close this gap without requiring a new command.

### 9. Feed Event Parsing Edge Cases

`parseGtEventLine` and `parseBdActivityLine` have no direct unit tests for
malformed input, missing fields, or unusual payloads. The print_events_test.go
tests cover the happy path through `PrintGtEvents` but don't exercise edge cases.

**Proposed change**: Add direct unit tests for event parsing functions.

## Testing Improvements (Implemented)

The following test files are added as part of this bead:

### `internal/cmd/feed_test.go`
- Tests for `buildFeedArgs` with various flag combinations
- Tests for feed command flag registration and defaults

### `internal/tui/feed/events_test.go`
- Direct tests for `parseGtEventLine` (valid, malformed, missing fields, visibility filtering)
- Direct tests for `buildEventMessage` (all event types, missing payloads)
- Direct tests for `parseBdActivityLine` (valid patterns, simple lines, empty input)
- Direct tests for `parseBeadContext`
- Direct tests for `matchesFilters` edge cases

### `internal/cmd/vitals_test.go` (extended)
- Tests for `findVitalsZombies` with mock data
- Tests for `vitalsStats` struct handling

## Priority Ranking

| # | Improvement | Impact | Effort | Priority |
|---|-------------|--------|--------|----------|
| 9 | Event parsing tests | High (reliability) | Low | P1 |
| 1 | Feed convoy filter | Medium (usability) | Low | P2 |
| 6 | Feed summary mode | Medium (usability) | Medium | P2 |
| 2 | Dashboard port fallback | Low (rare issue) | Low | P2 |
| 5 | Enriched session events | Medium (debuggability) | Medium | P2 |
| 4 | Vitals agent health | Medium (monitoring) | Medium | P3 |
| 3 | Status text truncation | Low (cosmetic) | Low | P3 |
| 7 | Convoy status detail | Medium (usability) | Medium | P3 |
| 8 | Unified MQ in convoy panel | Low (partial exists) | Medium | P3 |
