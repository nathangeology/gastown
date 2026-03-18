# gt sling Performance Profile & Speedup Recommendations

## Executive Summary

`gt sling` dispatches work to polecats through a 12-phase pipeline. Static
analysis of the code path reveals that **~60-70% of wall-clock time is spent
in Dolt round-trips and git operations**, with significant opportunities for
parallelization and elimination of redundant work.

The biggest wins come from:
1. **Parallelizing independent Dolt writes** (convoy + formula + hook)
2. **Eliminating redundant `git fetch origin`** (called 2-3× per sling)
3. **Lazy convoy creation** (defer to background)
4. **Batching Dolt operations** in formula instantiation

## Phase-by-Phase Breakdown

### Phase Map (sequential order)

```
runSling()
├── pre-checks              ~1ms    (flag validation, stdin, env)
├── sling-lock              ~1ms    (flock acquire)
├── bead-info-guards        ~50-200ms (bd show → Dolt query + status checks)
├── resolve-target          ~3-8s   ★ DOMINANT
│   └── SpawnPolecatForSling()
│       ├── find-workspace      ~1ms
│       ├── dolt-health         ~50-200ms  (bd show __health_check__)
│       ├── dolt-capacity       ~50-100ms  (Dolt SQL query)
│       ├── cap-checks          ~50-100ms  (countActivePolecats → tmux ls)
│       ├── find-idle           ~100-500ms (List polecats + beads queries)
│       └── allocate-and-add    ~2-5s   ★ EXPENSIVE
│           ├── pool-lock           ~1ms
│           ├── reconcile-pool      ~100-500ms (readdir + tmux has-session × N)
│           ├── fetch-origin        ~500ms-3s  ★ NETWORK
│           ├── worktree-add        ~200-500ms (git worktree add)
│           ├── setup-shared-beads  ~10-50ms
│           ├── provision-overlay   ~10-50ms
│           ├── runtime-settings    ~10-50ms
│           ├── setup-hooks         ~10-100ms (runs shell scripts)
│           └── agent-bead-create   ~100-500ms (Dolt write with retry)
├── auto-convoy             ~200-500ms (2 Dolt writes: create + dep add)
├── formula-guard-burn      ~50-100ms  (collectExistingMolecules)
├── formula-instantiate     ~500ms-2s  ★ EXPENSIVE
│   ├── cook                    ~50-200ms  (bd cook)
│   ├── wisp                    ~200-500ms (bd mol wisp → Dolt write)
│   └── bond                    ~200-500ms (bd mol bond → Dolt write)
├── hook-bead               ~100-300ms (bd update + verify → 2 Dolt ops)
├── store-fields            ~100-300ms (bd show + bd update → 2 Dolt ops)
├── session-start           ~5-30s  ★ DOMINANT
│   ├── tmux new-session        ~50ms
│   ├── WaitForCommand          ~2-5s   (shell startup)
│   ├── AcceptStartupDialogs    ~100ms
│   └── WaitForRuntimeReady     ~3-25s  (Claude/agent startup)
├── nudge                   ~10-50ms
```

### Estimated Total: ~10-40s (typical: ~15-20s)

## Bottleneck Analysis

### 1. Session Start (~5-30s) — 40-60% of total

The single largest phase. `WaitForRuntimeReady` polls for the agent's prompt
prefix (e.g., "❯ " for Claude) with a 30s timeout. This is inherently
sequential — the agent must be running before it can receive work.

**However**: Session start is already deferred until after formula/hook
attachment, which is correct. The wait is unavoidable for correctness.

**Recommendation**: Consider a "fire and forget" mode where sling returns
immediately after `tmux new-session` and lets the agent self-discover work
via `gt prime`. The `WaitForRuntimeReady` + `WaitForCommand` calls exist
primarily for nudge delivery reliability, which is less critical when the
agent has a SessionStart hook.

### 2. `git fetch origin` (~0.5-3s) — Called 2-3× redundantly

- Once in `addWithOptionsLocked` (or `ReuseIdlePolecat`)
- Once in `SpawnPolecatForSling` for idle polecat reuse path
- Potentially once more in the worktree's own fetch

Each fetch is a network round-trip. On slow connections this dominates.

**Recommendation**: Fetch once at the top of `SpawnPolecatForSling` and pass
a `skipFetch` flag to `addWithOptionsLocked` / `ReuseIdlePolecat`. The bare
repo fetch is sufficient — worktree inherits refs.

### 3. Dolt Round-Trips (~1-3s cumulative) — 6-10 separate operations

Each `bd` command spawns a subprocess that connects to the Dolt SQL server.
The sling path makes these sequential Dolt calls:

| # | Operation | Phase |
|---|-----------|-------|
| 1 | `bd show <bead>` (getBeadInfo) | bead-info-guards |
| 2 | `bd show __health_check__` | dolt-health |
| 3 | Dolt SQL (HasConnectionCapacity) | dolt-capacity |
| 4 | `bd` queries in FindIdlePolecat | find-idle |
| 5 | `bd` create agent bead | agent-bead-create |
| 6 | `bd create` convoy | auto-convoy |
| 7 | `bd dep add` convoy tracking | auto-convoy |
| 8 | `bd cook` formula | formula-cook |
| 9 | `bd mol wisp` | formula-wisp |
| 10| `bd mol bond` | formula-bond |
| 11| `bd update --status=hooked` | hook-bead |
| 12| `bd show` (verify hook) | hook-bead |
| 13| `bd show` (read for store) | store-fields |
| 14| `bd update` (write fields) | store-fields |

That's **14 Dolt round-trips**, each ~50-200ms. At 100ms average, that's
~1.4s just in Dolt overhead.

**Recommendation**: Batch operations where possible:
- Combine hook-bead + store-fields into a single `bd update` call
- Eliminate the verify read in hook-bead (trust the write, or use `--json`
  output from the update to confirm)
- Move auto-convoy to background (it's not on the critical path)

### 4. Auto-Convoy Creation (~200-500ms) — Parallelizable

Convoy creation (2 Dolt writes) is independent of formula instantiation and
hook attachment. It exists for dashboard visibility, not correctness.

**Recommendation**: Create convoy in a goroutine, or defer to a post-sling
background task. The convoy can be created after the polecat starts working.

### 5. Formula Instantiation (~0.5-2s) — 3 sequential bd calls

`cook` → `wisp` → `bond` are strictly sequential. Each spawns a `bd`
subprocess.

**Recommendation**:
- Skip `cook` when the formula is already cooked (check proto existence first)
- Consider a single `bd mol instantiate <formula> <bead>` command that does
  cook+wisp+bond atomically in one Dolt transaction
- The `--ephemeral` fallback path (`bondFormulaDirect`) already does this
  partially — make it the primary path

### 6. Pool Reconciliation (~100-500ms) — Scales with polecat count

`reconcilePoolInternal` calls `List()` (readdir) then checks tmux sessions
for each pooled name. With 20+ polecats, this adds up.

**Recommendation**: Cache reconciliation results with a short TTL (e.g., 5s).
Multiple slings in quick succession (batch mode) would benefit from not
re-reconciling the pool for each dispatch.

## Parallelization Opportunities

### Currently Sequential (must remain so)
- bead-info-guards → resolve-target (need bead info before spawning)
- resolve-target → formula-instantiate (need worktree path)
- formula-instantiate → hook-bead (need molecule ID)
- hook-bead → session-start (agent must see hook)

### Can Be Parallelized
```
After resolve-target completes:
  ┌─ auto-convoy (independent)
  ├─ formula-instantiate (needs worktree path only)
  └─ wake-rig-agents (independent)

After formula-instantiate:
  ┌─ hook-bead + store-fields (can be one operation)
  └─ (convoy still running in background)
```

### Within SpawnPolecatForSling
```
After find-workspace + load-rig:
  ┌─ dolt-health + dolt-capacity (can be one query)
  ├─ cap-checks (tmux ls, independent)
  └─ find-idle (beads query, independent)
```

## Specific Recommendations (Priority Order)

### P0: High Impact, Low Risk

1. **Eliminate redundant `git fetch origin`**
   - Save ~0.5-3s per sling
   - Fetch once in `SpawnPolecatForSling`, pass `skipFetch` to manager
   - Risk: None (fetch is idempotent)

2. **Combine hook-bead + store-fields into single `bd update`**
   - Save ~200-400ms (eliminate 2 Dolt round-trips)
   - The hook write and field storage can use one `bd update` call
   - Risk: Low (already doing read-modify-write in storeFieldsInBead)

3. **Skip hook verification read**
   - Save ~50-200ms per sling
   - Trust the `bd update` return code, or parse `--json` output
   - The retry loop already handles transient failures
   - Risk: Low (verification is defense-in-depth, not correctness)

### P1: High Impact, Medium Risk

4. **Defer auto-convoy to background goroutine**
   - Save ~200-500ms on critical path
   - Convoy is for dashboard visibility, not polecat correctness
   - Risk: Medium (convoy ID stored in bead fields — need to handle race)

5. **Merge dolt-health + dolt-capacity into one check**
   - Save ~50-200ms
   - Both are pre-spawn admission gates; one SQL query can check both
   - Risk: Low

6. **Add `bd mol instantiate` command (cook+wisp+bond in one call)**
   - Save ~200-500ms (eliminate 2 subprocess spawns + Dolt round-trips)
   - Requires `bd` CLI change
   - Risk: Medium (new bd command, but simplifies sling code)

### P2: Medium Impact, Higher Effort

7. **"Fire and forget" session start mode**
   - Save ~5-25s (skip WaitForRuntimeReady)
   - Agent discovers work via SessionStart hook → `gt prime`
   - Risk: Higher (nudge delivery less reliable; some agents may not
     have hooks configured)

8. **Cache pool reconciliation (5s TTL)**
   - Save ~100-500ms per sling in batch mode
   - Risk: Low (stale cache just means slightly outdated pool state)

9. **Parallelize pre-spawn checks**
   - Run dolt-health, cap-checks, find-idle concurrently
   - Save ~100-300ms
   - Risk: Medium (error handling for concurrent operations)

## Estimated Savings

| Optimization | Savings | Cumulative |
|-------------|---------|------------|
| Eliminate redundant fetch | 0.5-3s | 0.5-3s |
| Combine hook+store | 200-400ms | 0.7-3.4s |
| Skip hook verify | 50-200ms | 0.75-3.6s |
| Defer convoy | 200-500ms | 0.95-4.1s |
| Merge health checks | 50-200ms | 1.0-4.3s |
| bd mol instantiate | 200-500ms | 1.2-4.8s |
| Fire-and-forget session | 5-25s | 6.2-29.8s |

**Without fire-and-forget**: ~1-5s savings (10-30% improvement)
**With fire-and-forget**: ~6-30s savings (50-75% improvement)

## Profiling Instrumentation

This PR adds opt-in profiling via `GT_SLING_PROFILE=1`:

```bash
GT_SLING_PROFILE=1 gt sling gs-abc gastown
```

Output (to stderr):
```
⏱ gt sling profile (18.2s total)
────────────────────────────────────────────────────────────
  pre-checks                        1ms   0.0%
  sling-lock                        1ms   0.0%
  bead-info-guards                120ms   0.7% █
  resolve-target                 5200ms  28.6% ██████████████
  auto-convoy                     350ms   1.9% █
  formula-guard-burn               80ms   0.4%
  formula-instantiate            1200ms   6.6% ███
  hook-bead                       250ms   1.4% █
  store-fields                    200ms   1.1% █
  session-start                10800ms  59.3% █████████████████████████████
  nudge                            20ms   0.1%
────────────────────────────────────────────────────────────
  Sequential:      16.8s
  Parallelizable:  1.4s
```

Sub-phase profiling for `addWithOptionsLocked` is also included.

## How to Use the Profiler

1. Set `GT_SLING_PROFILE=1` in the environment
2. Run any `gt sling` command
3. Timing breakdown prints to stderr
4. Both `runSling` (top-level) and `SpawnPolecatForSling` / `addWithOptionsLocked`
   (sub-phases) are instrumented

The profiler has zero overhead when disabled (env var not set).
