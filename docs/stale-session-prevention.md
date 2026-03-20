# Stale Session Prevention (gs-qoi)

## Problem

Refinery and witness kiro-cli sessions crash silently but leave the tmux session
alive with a stale prompt. `gt up` sees the tmux session exists and skips restart.
Nudges are delivered but never processed. Completed polecat branches sit unmerged
indefinitely.

## Root Cause

Two detection mechanisms exist for zombie sessions:

1. **Process liveness** (`IsAgentAlive`): Checks if the agent process tree is
   alive inside the tmux session. This catches agent-dead zombies (process
   crashed, tmux alive). Used by `Manager.Start()` for all roles.

2. **Activity staleness** (`GetSessionActivity`): Checks tmux's
   `session_activity` timestamp. This was **removed** for witnesses and
   refineries due to the "serial killer bug" — idle witnesses/refineries
   legitimately produce no tmux output while waiting for work, so
   activity-based detection killed healthy sessions.

The gap: when kiro-cli crashes but the shell process survives (or the process
tree check sees a surviving shell), `IsAgentAlive` returns true. The session
appears healthy but the agent is unresponsive. With activity-based detection
removed, nothing catches this state.

## Solution

Extend the existing heartbeat system (used by polecats) to witness and refinery
sessions. Heartbeats are written by `gt` commands on every invocation — they
reflect actual agent activity, not tmux output activity.

### Changes

1. **`internal/cmd/root.go`**: Extended `touchPolecatHeartbeat()` to also write
   heartbeats for witness and refinery roles. Every `gt` command invocation
   updates the heartbeat file.

2. **`internal/daemon/daemon.go`**: Added `killStaleRoleSession()` which checks
   heartbeat freshness before `Manager.Start()` for witnesses and refineries.
   If the heartbeat is stale (>3 minutes), the session is killed so `Start()`
   recreates it.

### Why heartbeats avoid the serial killer bug

The serial killer bug occurred because tmux `session_activity` only updates on
terminal output. Idle agents waiting for work produce no output, so they appear
hung. Heartbeats are different — they're written by `gt` commands that the agent
runs periodically (hook checks, mail checks, prime). An agent that's alive and
responsive will always have a fresh heartbeat, even if it's idle.

### Rollout safety

Sessions started before this change won't have heartbeat files. The stale
detection explicitly checks for heartbeat file existence and skips sessions
without one (`exists == false → no-op`). This prevents false positives during
the rollout period. Once all sessions have been restarted with the new code,
heartbeat files will be present for all roles.

### Threshold

Uses the existing `SessionHeartbeatStaleThreshold` (3 minutes) from
`internal/polecat/heartbeat.go`. This is the same threshold used for polecat
stale detection and provides a reasonable balance between detection speed and
false positive avoidance.
