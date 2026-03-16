package startupmsg

import (
	"strings"

	"github.com/steveyegge/gastown/internal/cli"
)

// HookAndMailInstructions returns the deterministic startup checklist for
// sessions that need to inspect hook and inbox state before acting.
func HookAndMailInstructions() string {
	return "Run these commands in order:\n" +
		"1. `" + cli.Name() + " hook` - inspect hooked work\n" +
		"2. `" + cli.Name() + " mail inbox` - inspect queued messages\n" +
		"3. If work is hooked -> execute it immediately\n" +
		"4. If nothing is hooked -> wait for instructions"
}

// AssignedHookInstructions returns the deterministic startup checklist for
// agents that were started specifically to work on hooked work.
func AssignedHookInstructions() string {
	return "Run these commands in order:\n" +
		"1. `" + cli.Name() + " prime --hook` - load role context and hook state\n" +
		"2. `" + cli.Name() + " hook` - confirm the hooked work\n" +
		"3. Execute the hooked work immediately"
}

// StartupNudgeInstructions returns the fallback command-oriented nudge used when
// the runtime cannot rely on prompt-time beacon instructions alone.
func StartupNudgeInstructions() string {
	return "Run these commands now:\n" +
		"1. `" + cli.Name() + " hook`\n" +
		"2. If work is hooked -> execute it immediately\n" +
		"3. If nothing is hooked -> `" + cli.Name() + " mail inbox`"
}

// HookedWorkStartInstructions returns a deterministic start nudge for a known
// bead assignment. Subject and args are optional context lines.
func HookedWorkStartInstructions(beadID, subject, args string) string {
	lines := []string{
		"Run these commands now:",
		"1. `" + cli.Name() + " hook`",
		"2. Confirm the hooked bead is `" + beadID + "`",
		"3. `bd show " + beadID + "`",
		"4. Execute `" + beadID + "` immediately",
	}
	if subject != "" {
		lines = append(lines, "Context: "+subject)
	}
	if args != "" {
		lines = append(lines, "Args: "+args)
	}
	return strings.Join(lines, "\n")
}
