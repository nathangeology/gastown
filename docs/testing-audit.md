# Testing Audit: Gas Town Agent Communication and Coordination

**Date:** 2026-03-17
**Auditor:** polecat furiosa
**Scope:** `internal/` packages — agent communication, coordination, state machines, integration seams

## Executive Summary

Gas Town has strong test infrastructure: 447 test files covering 555 source files, with extensive use of temp directories (1,899 `t.TempDir` calls), fakes/mocks (110 test files), and integration tests (~20 files). The codebase already validates state machine transitions in the refinery and has crash-recovery tests across multiple packages.

The primary gaps are:
1. **Tmux session management** — 3,388 LOC with heavy `exec.Command` usage and no abstraction layer for testing
2. **Cross-agent coordination** — no end-to-end tests for multi-agent workflows (sling → work → done → refinery merge)
3. **Mail delivery reliability** — only 3 delivery-specific tests despite a two-phase delivery protocol
4. **Time-dependent test fragility** — 122 `time.Sleep` calls in tests creating flakiness risk
5. **Polecat lifecycle state machine** — states defined but no `ValidateTransition` equivalent (unlike refinery)

## 1. Agent Communication Reliability

### Mail System (`internal/mail/`)

**Current coverage:** 114 tests across 7 test files. Router (38 tests) and types (32 tests) are well-covered.

**Gaps:**
- `delivery.go` has only 3 tests despite implementing a two-phase commit protocol (pending → acked). The crash-safety properties of the label ordering are documented but not tested under simulated crash scenarios.
- `mailbox.go` (31K source) has 24 tests — reasonable but the send-receive-ack lifecycle isn't tested as an integrated flow.
- No tests for mail delivery failure modes: what happens when the recipient's beads DB is unavailable? When Dolt commits fail mid-delivery?
- No tests for concurrent mail delivery to the same recipient.

**Recommendation (P1):** Add crash-simulation tests for the two-phase delivery protocol — kill between phase-1 (pending label) and phase-2 (ack labels), verify recovery produces correct state.

### Nudge Mechanics (`internal/nudge/`)

**Current coverage:** 30 tests (20 queue, 10 poller). Queue logic is well-tested.

**Gaps:**
- `poller.go` spawns OS processes (`exec.Command`) with no abstraction — tests can't verify nudge delivery without real tmux sessions.
- No tests for nudge deduplication under rapid-fire conditions.
- Platform-specific `procattr_unix.go` / `procattr_windows.go` have no tests.

**Recommendation (P2):** Extract a `ProcessSpawner` interface from the poller to enable testing nudge delivery without real processes.

### Hook Handoffs (`internal/cmd/hook.go`, `internal/hooks/`)

**Current coverage:** Hook commands have 3 unit tests + 2 integration tests (slot, show). Hooks config has 30 tests.

**Gaps:**
- The hook → prime → work → done lifecycle is never tested as a connected flow.
- `hook.go` (22K) has only 3 tests — the hook resolution logic (finding hooked beads, molecules, mail) is undertested.
- No tests for hook contention (two agents trying to hook the same bead).

**Recommendation (P2):** Add integration tests for the hook lifecycle: set hook → verify prime reads it → verify done clears it.

## 2. Command Contract Testing

### Well-Tested Commands
- `sling` — 73K test file + batch tests (58K) + multiple focused test files. Excellent coverage.
- `convoy` — 97K stage tests + property tests + multiple focused files. Best-tested subsystem.
- `done` — 43K test file + 30K closeDescendants tests. Good coverage.
- `handoff` — 26K test file. Good coverage.

### Under-Tested Commands
| Command | Source LOC | Test LOC | Test:Source Ratio | Gap |
|---------|-----------|----------|-------------------|-----|
| `hook.go` | 22,522 | 3,231 | 0.14 | Hook resolution, molecule attachment |
| `prime.go` | 42,042 | 29,143 | 0.69 | Good ratio but prime_molecule (15K) has 0 direct tests |
| `nudge.go` | 31,461 | 13,805 | 0.44 | Delivery mechanics, tmux injection |
| `witness.go` | 10,513 | 942 | 0.09 | Almost no tests for witness command |
| `refinery.go` | 23,169 | 1,399 | 0.06 | Command layer barely tested (engine is tested) |
| `deacon.go` | 52,534 | 6,031 | 0.11 | Status tests only; main deacon loop untested at cmd level |
| `polecat.go` | 56,244 | 4,772+1,170 | 0.11 | Dotdir and list-state tests only |

**Recommendation (P1):** Add contract tests for `witness`, `refinery`, and `deacon` commands — these are the coordination backbone. At minimum, test that each command's flags parse correctly and produce expected output shapes.

### Input/Output Contracts

The `bd_helpers.go` and `bd_helpers_test.go` (13K tests) show good patterns for testing bd CLI integration. However:
- No schema validation for JSON output of commands (e.g., `gt status --json`, `gt hook --json`).
- No contract tests ensuring command output format stability across versions.

**Recommendation (P3):** Add golden-file tests for JSON output of key commands to catch accidental format changes.

## 3. State Machine Coverage

### Refinery MR State Machine — GOOD ✓

`internal/refinery/types.go` defines explicit `ValidateTransition` and `ValidatePhaseTransition` functions with a `ValidPhaseTransitions` map. Tests in `types_test.go` (7.6K) cover valid and invalid transitions. This is the gold standard in the codebase.

### Polecat Lifecycle State Machine — GAP

`internal/polecat/types.go` defines 5 states (`working`, `idle`, `done`, `stuck`, `zombie`) but has **no transition validation function**. State changes go through `SetState`/`SetAgentState` which accept any string — invalid transitions are not caught.

**Gaps:**
- No equivalent of `ValidateTransition` for polecat states.
- No tests for invalid state transitions (e.g., `idle → zombie` directly).
- The `types_test.go` (2.7K) tests helper methods but not transition validity.
- "Stalled" and "zombie" are described as "detected conditions, not stored states" — but `StateZombie` IS a stored constant. This ambiguity is untested.

**Recommendation (P1):** Add a `ValidatePolecatTransition` function with an explicit transition map, matching the refinery pattern. Add tests for all valid/invalid transitions.

### Dolt Lifecycle (CREATE → LIVE → CLOSE → DECAY → COMPACT → FLATTEN)

`internal/daemon/lifecycle.go` (46K) implements the six-stage lifecycle. `lifecycle_test.go` (12K) and `lifecycle_defaults_test.go` (11K) provide reasonable coverage.

**Gap:** No property-based tests for lifecycle stage ordering invariants.

### Beads Status Transitions

`internal/beads/status.go` (3.1K) defines valid statuses with tests (3.6K). Reasonable coverage.

## 4. Integration Seams

### Tmux Session Management — CRITICAL GAP

`internal/tmux/tmux.go` is 3,388 LOC with **direct `exec.Command("tmux", ...)` calls throughout**. There is no `TmuxClient` interface or abstraction layer.

**Impact:**
- Tests require a real tmux server (117 tests exist, but they're slow and environment-dependent).
- The `testmain_test.go` sets up a real tmux socket for tests.
- Functions like `KillSession`, `SendKeys`, `NewSession`, `ListSessions` all shell out directly.
- 20+ `exec.Command("kill", ...)` calls for process management with no abstraction.

**Current mitigations:** Some packages define narrow interfaces (`deacon/manager.go` has `tmuxOps`, `doctor/` has `SessionLister`, `tmuxRenamer`). But the core `tmux` package itself has no interface.

**Recommendation (P1):** Define a `TmuxClient` interface in the `tmux` package covering the ~10 core operations (NewSession, KillSession, ListSessions, SendKeys, HasSession, etc.). Provide a `FakeTmux` implementation for tests. This unblocks unit testing of every package that currently shells out to tmux.

### Git Worktree Operations (`internal/git/`)

**Current coverage:** 0.75 test:source ratio. Good.

**Gap:** Worktree creation/deletion in `internal/cmd/worktree.go` (10K) has no direct tests. The worktree lifecycle (create for sling → use during work → delete on done) is tested only indirectly through sling/done tests.

### Dolt Persistence (`internal/doltserver/`)

**Current coverage:** 126K test file for 124K source. Excellent 1:1 ratio. Includes crash recovery tests, migration tests, and conformance tests.

**Gap:** The `wl_commons.go` store interface is well-defined but only has 3 test files. The conformance test pattern (`wl_commons_conformance_test.go`) is good — extend it.

## 5. Failure Mode Testing

### Agent Crash Recovery — PARTIAL

**Tested:**
- Dolt mid-migration crash recovery (2 tests in `doltserver_test.go`)
- Mail delivery crash states (1 test in `delivery_test.go`)
- Convoy manager recovery mode (3 tests in `convoy_manager_test.go`)
- Dog manager partial state recovery (1 integration test)

**Not tested:**
- Polecat crash mid-`gt done` (branch pushed but MR bead not created)
- Witness crash during patrol (stale polecat detection interrupted)
- Refinery crash mid-merge (branch merged but bead not closed)
- Session death during `gt handoff` (mail created but session not cycled)

**Recommendation (P1):** Add crash-point tests for the three critical paths: `gt done`, refinery merge, and witness patrol. Use the existing Dolt crash-recovery pattern as a template.

### Stale Polecat Detection

**Tested:** 219 test functions mention crash/recovery/orphan/stale/zombie across the codebase. The `doctor/` package has extensive checks (orphan_check, zombie_check, stale_agent_beads_check, etc.).

**Gap:** The witness's stale-polecat detection logic (`internal/witness/handlers.go`, 99K) has 80 tests but no tests simulating a polecat that stops heartbeating mid-work.

### Orphaned Bead Cleanup

**Tested:** `internal/cmd/orphans.go` (31K) with `orphans_test.go` (8.6K). Doctor has `orphan_check.go` with 13K tests.

**Gap:** No test for the full orphan lifecycle: polecat dies → witness detects → bead reset to open → re-dispatched.

### Mail Delivery Failures

**Gap:** No tests for:
- Sending mail when recipient's Dolt DB is down
- Mail queue overflow
- Duplicate mail detection

## 6. Prioritized Recommendations

### P0 — Critical (blocks reliability)

1. **Tmux abstraction layer** — Define `TmuxClient` interface, implement `FakeTmux`. Unblocks testability of polecat, witness, deacon, and nudge packages. Estimated impact: enables 50+ new unit tests across 6 packages.

### P1 — High (significant reliability improvement)

2. **Polecat state machine validation** — Add `ValidatePolecatTransition` matching the refinery pattern. Prevents invalid state transitions that could leave polecats in limbo.

3. **Crash-point tests for gt done** — Simulate failure at each step (push, MR create, bead update, session kill). Verify recovery produces correct state.

4. **Mail delivery crash tests** — Test the two-phase protocol under simulated crashes between phase-1 and phase-2.

5. **Witness/refinery/deacon command contract tests** — These coordination commands have <0.12 test:source ratio. Add basic flag parsing and output shape tests.

### P2 — Medium (improved confidence)

6. **Cross-agent integration test** — A single test that exercises: sling → polecat works → gt done → refinery merges → bead closed. Can use the existing testutil infrastructure with a temp town.

7. **Nudge process spawner interface** — Extract from poller for testability.

8. **Hook lifecycle integration test** — Test hook → prime → work → done as connected flow.

9. **Eliminate time.Sleep in tests** — 122 occurrences. Replace with polling/channel-based synchronization to reduce flakiness.

### P3 — Low (polish)

10. **Property-based tests for state machines** — Extend the convoy property test pattern to polecat and refinery state machines. Verify invariants like "no state is unreachable" and "terminal states have no outgoing transitions."

11. **Golden-file tests for JSON output** — Catch accidental format changes in `--json` output.

12. **Platform-specific code tests** — `procattr_unix.go`, `procattr_windows.go`, `process_group_*.go` have no tests.

## Appendix: Coverage by Package

| Package | Source (bytes) | Test (bytes) | Ratio | Tests | Assessment |
|---------|---------------|-------------|-------|-------|------------|
| cmd | 2,827,929 | 1,407,052 | 0.49 | ~500+ | Good overall, gaps in witness/refinery/deacon cmds |
| daemon | 368,781 | 272,371 | 0.73 | ~100+ | Good |
| witness | 153,355 | 112,478 | 0.73 | 156 | Good for handlers, weak for manager |
| polecat | 150,356 | 120,396 | 0.80 | ~80 | Good ratio, missing state validation |
| tmux | 131,862 | 99,606 | 0.75 | 117 | Good count but requires real tmux |
| refinery | 124,048 | 81,623 | 0.65 | ~60 | Engine good, command layer weak |
| doltserver | 124,535 | 126,827 | 1.02 | ~80 | Excellent |
| config | 85,435+ | 156,142+ | 1.0+ | ~100+ | Excellent |
| mail | 60,937+ | 51,833+ | 0.68 | 114 | Good overall, delivery gap |
| doctor | ~200K | ~200K | ~1.0 | ~150 | Excellent |
| beads | ~200K | ~150K | 0.75 | ~100 | Good |
| nudge | 12,223+ | 20,930+ | 0.83 | 30 | Good for queue, poller needs interface |
| protocol | 45,489 | 21,401 | 0.47 | 31 | Below average |
| reaper | 29,922 | 7,317 | 0.24 | 9 | Significantly under-tested |
| constants | 16,270 | 3,247 | 0.19 | Low | Under-tested |
| agentlog | 14,851 | 3,502 | 0.23 | Low | Under-tested |
