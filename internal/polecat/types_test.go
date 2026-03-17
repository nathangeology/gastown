package polecat

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestState_IsWorking(t *testing.T) {
	tests := []struct {
		state  State
		expect bool
	}{
		{StateWorking, true},
		{StateDone, false},
		{StateStuck, false},
		{StateZombie, false},
		{State("unknown"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsWorking(); got != tt.expect {
				t.Errorf("State(%q).IsWorking() = %v, want %v", tt.state, got, tt.expect)
			}
		})
	}
}

func TestPolecat_Summary(t *testing.T) {
	now := time.Now()
	p := &Polecat{
		Name:      "alpha",
		Rig:       "gastown",
		State:     StateWorking,
		ClonePath: "/some/path",
		Branch:    "polecat/alpha",
		Issue:     "gt-123",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s := p.Summary()
	if s.Name != "alpha" {
		t.Errorf("Summary.Name = %q, want %q", s.Name, "alpha")
	}
	if s.State != StateWorking {
		t.Errorf("Summary.State = %q, want %q", s.State, StateWorking)
	}
	if s.Issue != "gt-123" {
		t.Errorf("Summary.Issue = %q, want %q", s.Issue, "gt-123")
	}
}

func TestPolecat_Summary_NoIssue(t *testing.T) {
	p := &Polecat{
		Name:  "beta",
		State: StateDone,
	}

	s := p.Summary()
	if s.Issue != "" {
		t.Errorf("Summary.Issue = %q, want empty", s.Issue)
	}
}

func TestCleanupStatus_IsSafe(t *testing.T) {
	tests := []struct {
		status CleanupStatus
		expect bool
	}{
		{CleanupClean, true},
		{CleanupUncommitted, false},
		{CleanupStash, false},
		{CleanupUnpushed, false},
		{CleanupUnknown, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsSafe(); got != tt.expect {
				t.Errorf("CleanupStatus(%q).IsSafe() = %v, want %v", tt.status, got, tt.expect)
			}
		})
	}
}

func TestCleanupStatus_RequiresRecovery(t *testing.T) {
	tests := []struct {
		status CleanupStatus
		expect bool
	}{
		{CleanupClean, false},
		{CleanupUncommitted, true},
		{CleanupStash, true},
		{CleanupUnpushed, true},
		{CleanupUnknown, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.RequiresRecovery(); got != tt.expect {
				t.Errorf("CleanupStatus(%q).RequiresRecovery() = %v, want %v", tt.status, got, tt.expect)
			}
		})
	}
}

func TestCleanupStatus_CanForceRemove(t *testing.T) {
	tests := []struct {
		status CleanupStatus
		expect bool
	}{
		{CleanupClean, true},
		{CleanupUncommitted, true},
		{CleanupStash, false},
		{CleanupUnpushed, false},
		{CleanupUnknown, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.CanForceRemove(); got != tt.expect {
				t.Errorf("CleanupStatus(%q).CanForceRemove() = %v, want %v", tt.status, got, tt.expect)
			}
		})
	}
}

func TestValidatePolecatTransition_Valid(t *testing.T) {
	for from, targets := range ValidPolecatTransitions {
		for _, to := range targets {
			t.Run(fmt.Sprintf("%s→%s", from, to), func(t *testing.T) {
				if err := ValidatePolecatTransition(from, to); err != nil {
					t.Errorf("ValidatePolecatTransition(%s, %s) = %v, want nil", from, to, err)
				}
			})
		}
	}
}

func TestValidatePolecatTransition_SameState(t *testing.T) {
	for _, s := range []State{StateWorking, StateIdle, StateDone, StateStuck, StateZombie} {
		t.Run(string(s), func(t *testing.T) {
			if err := ValidatePolecatTransition(s, s); err != nil {
				t.Errorf("same-state transition %s→%s should be allowed: %v", s, s, err)
			}
		})
	}
}

func TestValidatePolecatTransition_Invalid(t *testing.T) {
	tests := []struct {
		from, to State
	}{
		{StateIdle, StateZombie},
		{StateIdle, StateDone},
		{StateIdle, StateStuck},
		{StateDone, StateWorking},
		{StateDone, StateStuck},
		{StateZombie, StateWorking},
		{StateZombie, StateDone},
		{StateWorking, StateZombie},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s→%s", tt.from, tt.to), func(t *testing.T) {
			err := ValidatePolecatTransition(tt.from, tt.to)
			if err == nil {
				t.Errorf("ValidatePolecatTransition(%s, %s) = nil, want error", tt.from, tt.to)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("error should wrap ErrInvalidTransition, got: %v", err)
			}
		})
	}
}

func TestValidatePolecatTransition_UnknownState(t *testing.T) {
	err := ValidatePolecatTransition(State("bogus"), StateWorking)
	if err == nil {
		t.Error("expected error for unknown source state")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error should wrap ErrInvalidTransition, got: %v", err)
	}
}
