package cmd

import (
	"path/filepath"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
)

func resolveAgentWork(ctx RoleContext, agentID string) *beads.Issue {
	if agentID == "" {
		return nil
	}

	primary := beads.New(rigBeadsRoot(ctx))
	assigned := findActiveAssignedWork(primary, agentID)

	if len(assigned) == 0 && !isTownLevelRole(agentID) && ctx.TownRoot != "" {
		townB := beads.New(filepath.Join(ctx.TownRoot, ".beads"))
		assigned = findActiveAssignedWork(townB, agentID)
	}

	legacyHook := findLegacyHookBead(ctx, agentID)
	branchIssue := findBranchRecoveredWork(ctx)

	return selectCurrentAgentWork(agentID, assigned, legacyHook, branchIssue)
}

func findActiveAssignedWork(b *beads.Beads, agentID string) []*beads.Issue {
	var issues []*beads.Issue

	hooked, err := b.List(beads.ListOptions{
		Status:   beads.StatusHooked,
		Assignee: agentID,
		Priority: -1,
	})
	if err == nil {
		issues = append(issues, hooked...)
	}

	inProgress, err := b.List(beads.ListOptions{
		Status:   "in_progress",
		Assignee: agentID,
		Priority: -1,
	})
	if err == nil {
		issues = append(issues, inProgress...)
	}

	return issues
}

func findLegacyHookBead(ctx RoleContext, agentID string) *beads.Issue {
	agentBeadID := buildAgentBeadID(agentID, ctx.Role, ctx.TownRoot)
	if agentBeadID == "" {
		return nil
	}

	agentBeadDir := beads.ResolveHookDir(ctx.TownRoot, agentBeadID, ctx.WorkDir)
	ab := beads.New(agentBeadDir)
	agentBead, err := ab.Show(agentBeadID)
	if err != nil || agentBead == nil || agentBead.HookBead == "" {
		return nil
	}

	hookBeadDir := beads.ResolveHookDir(ctx.TownRoot, agentBead.HookBead, ctx.WorkDir)
	hb := beads.New(hookBeadDir)
	hookBead, err := hb.Show(agentBead.HookBead)
	if err != nil {
		return nil
	}
	return hookBead
}

func findBranchRecoveredWork(ctx RoleContext) *beads.Issue {
	if ctx.WorkDir == "" || (ctx.Role != RolePolecat && ctx.Role != RoleCrew) {
		return nil
	}

	g := git.NewGit(ctx.WorkDir)
	branch, err := g.CurrentBranch()
	if err != nil || branch == "" {
		return nil
	}

	info := parseBranchName(branch)
	if info.Issue == "" {
		return nil
	}

	issueDir := beads.ResolveHookDir(ctx.TownRoot, info.Issue, rigBeadsRoot(ctx))
	b := beads.New(issueDir)
	issue, err := b.Show(info.Issue)
	if err != nil || issue == nil {
		return nil
	}
	if issue.Status == "closed" || issue.Status == "tombstone" {
		return nil
	}
	return issue
}

func selectCurrentAgentWork(agentID string, assigned []*beads.Issue, legacyHook *beads.Issue, branchIssue *beads.Issue) *beads.Issue {
	for _, issue := range assigned {
		if isActiveAssignedIssueForAssignee(issue, agentID) {
			return issue
		}
	}

	if isActiveAssignedIssueForAssignee(legacyHook, agentID) {
		return legacyHook
	}

	if branchIssue != nil {
		return branchIssue
	}

	if isActiveIssue(legacyHook) {
		return legacyHook
	}

	return nil
}

func isActiveAssignedIssueForAssignee(issue *beads.Issue, agentID string) bool {
	return issue != nil &&
		isActiveIssue(issue) &&
		issue.Assignee == agentID
}

func isActiveIssue(issue *beads.Issue) bool {
	return issue != nil &&
		(issue.Status == beads.StatusHooked || issue.Status == "in_progress")
}
