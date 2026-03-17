// Package polecat provides polecat lifecycle management.
package polecat

import (
	"errors"
	"fmt"
	"time"
)

// State represents the current lifecycle state of a polecat.
//
// Polecats are PERSISTENT: they survive work completion and can be reused.
// The four operating states are:
//
//   - Working: Session active, doing assigned work (normal operation)
//   - Idle: Work completed, session killed, sandbox preserved for reuse
//   - Stalled: Session stopped unexpectedly, was never nudged back to life
//   - Zombie: Session called 'gt done' but cleanup failed - tried to die but couldn't
//
// The distinction matters: idle polecats completed their work successfully and
// are ready for new assignments. Stalled polecats failed mid-work. Zombies
// tried to exit but couldn't complete cleanup.
//
// Note: These are LIFECYCLE states. The polecat IDENTITY (CV chain, mailbox, work
// history) and SANDBOX (worktree) persist across sessions. An idle polecat keeps
// its worktree so it can be quickly reassigned without creating a new one.
//
// "Stalled" is a detected condition, not a stored state. The Witness detects it
// through monitoring (tmux state, heartbeat age, etc.). "Zombie" is both a
// detected condition AND a stored state — the Witness sets StateZombie when it
// finds an orphaned session, enabling transition validation.
type State string

const (
	// StateWorking means the polecat session is actively working on an issue.
	// This is the initial and primary state after sling.
	StateWorking State = "working"

	// StateIdle means the polecat completed its work and the session was killed,
	// but the sandbox (worktree) is preserved for reuse. An idle polecat has no
	// hook_bead and no active session. It can be reassigned via gt sling without
	// creating a new worktree.
	StateIdle State = "idle"

	// StateDone means the polecat has completed its assigned work and called
	// 'gt done'. This is normally a transient state - the session should exit
	// immediately after. If a polecat remains in StateDone, it's a "zombie":
	// the cleanup failed and the session is stuck.
	StateDone State = "done"

	// StateStuck means the polecat has explicitly signaled it needs assistance.
	// This is an intentional request for help from the polecat itself.
	// Different from "stalled" (detected externally when session stops working).
	StateStuck State = "stuck"

	// StateZombie means a tmux session exists but has no corresponding worktree directory.
	// This is both a detected condition (Witness discovers orphaned sessions) AND a stored
	// state (set explicitly when detection occurs). The Witness transitions a polecat to
	// StateZombie when it detects the condition, then to StateIdle after recovery.
	StateZombie State = "zombie"
)

// IsWorking returns true if the polecat is currently working.
func (s State) IsWorking() bool {
	return s == StateWorking
}

// IsIdle returns true if the polecat has completed work and is available for reuse.
func (s State) IsIdle() bool {
	return s == StateIdle
}

// ErrInvalidTransition is returned when a polecat state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid polecat state transition")

// ValidPolecatTransitions defines the allowed state transitions for polecat lifecycle.
//
// The lifecycle is:
//
//	working → done (completed work, called gt done)
//	working → stuck (explicitly signaled needs help)
//	working → idle (session killed by witness after work complete)
//	done → idle (cleanup succeeded, ready for reuse)
//	done → zombie (cleanup failed, session stuck)
//	stuck → working (unstuck, resumed work)
//	stuck → idle (witness killed session)
//	idle → working (reassigned new work via gt sling)
//	zombie → idle (witness recovered the zombie)
//
// Terminal note: zombie is recoverable (witness can clean it up), so it is not
// truly terminal. No state is permanently terminal — idle polecats get reused.
var ValidPolecatTransitions = map[State][]State{
	StateWorking: {StateDone, StateStuck, StateIdle},
	StateDone:    {StateIdle, StateZombie},
	StateStuck:   {StateWorking, StateIdle},
	StateIdle:    {StateWorking},
	StateZombie:  {StateIdle},
}

// ValidatePolecatTransition checks if a state transition is allowed.
func ValidatePolecatTransition(from, to State) error {
	if from == to {
		return nil
	}
	allowed, ok := ValidPolecatTransitions[from]
	if !ok {
		return fmt.Errorf("%w: unknown state %s", ErrInvalidTransition, from)
	}
	for _, target := range allowed {
		if target == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
}

// Polecat represents a worker agent in a rig.
type Polecat struct {
	// Name is the polecat identifier.
	Name string `json:"name"`

	// Rig is the rig this polecat belongs to.
	Rig string `json:"rig"`

	// State is the current lifecycle state.
	State State `json:"state"`

	// ClonePath is the path to the polecat's clone of the rig.
	ClonePath string `json:"clone_path"`

	// Branch is the current git branch.
	Branch string `json:"branch"`

	// Issue is the currently assigned issue ID (if any).
	Issue string `json:"issue,omitempty"`

	// CreatedAt is when the polecat was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the polecat was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// Summary provides a concise view of polecat status.
type Summary struct {
	Name  string `json:"name"`
	State State  `json:"state"`
	Issue string `json:"issue,omitempty"`
}

// Summary returns a Summary for this polecat.
func (p *Polecat) Summary() Summary {
	return Summary{
		Name:  p.Name,
		State: p.State,
		Issue: p.Issue,
	}
}

// CleanupStatus represents the git state of a polecat for cleanup decisions.
// The Witness uses this to determine whether it's safe to nuke a polecat worktree.
type CleanupStatus string

const (
	// CleanupClean means the worktree has no uncommitted work and is safe to remove.
	CleanupClean CleanupStatus = "clean"

	// CleanupUncommitted means there are uncommitted changes in the worktree.
	CleanupUncommitted CleanupStatus = "has_uncommitted"

	// CleanupStash means there are stashed changes that would be lost.
	CleanupStash CleanupStatus = "has_stash"

	// CleanupUnpushed means there are commits not pushed to the remote.
	CleanupUnpushed CleanupStatus = "has_unpushed"

	// CleanupUnknown means the status could not be determined.
	CleanupUnknown CleanupStatus = "unknown"
)

// IsSafe returns true if the status indicates it's safe to remove the worktree
// without losing any work.
func (s CleanupStatus) IsSafe() bool {
	return s == CleanupClean
}

// RequiresRecovery returns true if the status indicates there is work that
// needs to be recovered before removal. This includes uncommitted changes,
// stashes, and unpushed commits.
func (s CleanupStatus) RequiresRecovery() bool {
	switch s {
	case CleanupUncommitted, CleanupStash, CleanupUnpushed:
		return true
	default:
		return false
	}
}

// CanForceRemove returns true if the status allows forced removal.
// Uncommitted changes can be force-removed, but stashes and unpushed commits cannot.
func (s CleanupStatus) CanForceRemove() bool {
	return s == CleanupClean || s == CleanupUncommitted
}
