# Kiro CLI Runtime Integration

This document describes the integration of Kiro CLI as a supported AI agent runtime in Gas Town.

## Overview

Kiro CLI has been added as a fully supported agent runtime alongside Claude, Gemini, OpenCode, Copilot, and other agents. The integration follows the same architectural patterns as other non-Claude agents, with informational hooks (instructions file) rather than executable lifecycle hooks.

## Implementation Details

### 1. Agent Preset Configuration

**Location:** `internal/config/agents.go`

The Kiro preset is registered in the `builtinPresets` map with the following configuration:

```go
AgentKiro: {
    Name:                AgentKiro,
    Command:             "kiro-cli",
    Args:                []string{"chat"},
    ProcessNames:        []string{"kiro-cli", "kiro-cli-chat", "zsh"},
    SessionIDEnv:        "",  // TBD - may support in future
    ResumeFlag:          "",  // No resume support discovered yet
    ResumeStyle:         "",
    SupportsHooks:       false,
    SupportsForkSession: false,
    NonInteractive: &NonInteractiveConfig{
        Subcommand: "chat",
        PromptFlag: "--agent",
    },
    PromptMode:         "arg",
    ConfigDir:          ".kiro",
    HooksProvider:      "kiro",
    HooksDir:           ".kiro",
    HooksSettingsFile:  "kiro-instructions.md",
    HooksInformational: true,
    ReadyDelayMs:       5000,
    InstructionsFile:   "AGENTS.md",
}
```

### 2. Kiro Package

**Location:** `internal/kiro/`

Contains the settings management for Kiro CLI:

- `settings.go` - Main settings provisioning logic
- `settings_test.go` - Unit tests
- `plugin/gastown-instructions.md` - Gas Town instructions template for Kiro agents

The package provides the `EnsureSettingsAt()` function that installs the Gas Town instructions file into the agent's working directory at `.kiro/kiro-instructions.md`.

### 3. Runtime Registration

**Location:** `internal/runtime/runtime.go`

The Kiro hook installer is registered in the `init()` function:

```go
config.RegisterHookInstaller("kiro", func(settingsDir, workDir, role, hooksDir, hooksFile string) error {
    return kiro.EnsureSettingsAt(workDir, hooksDir, hooksFile)
})
```

### 4. Instructions Template

**Location:** `internal/kiro/plugin/gastown-instructions.md`

Provides comprehensive Gas Town workflow instructions for Kiro agents, including:
- Role descriptions
- Gas Town command reference
- Session startup workflow
- Coordination protocols
- Tool approval guidelines

## Usage

### Starting a Kiro Agent

To use Kiro CLI as the agent runtime, configure your role to use the "kiro" agent:

```bash
# Example: Start a crew role with kiro
gt crew start --agent kiro
```

### Configuration

Kiro-specific configuration can be provided through:

1. **Built-in preset** - Default configuration (already implemented)
2. **Custom agents.json** - Override defaults in town or rig settings
3. **Environment variables** - Runtime environment configuration

### Process Detection

Gas Town detects Kiro processes using the following process names:
- `kiro-cli`
- `kiro-cli-chat`
- `zsh` (when running kiro-cli-term)

## Features

### Supported
- ✅ Basic chat mode
- ✅ Agent selection via `--agent` flag
- ✅ Non-interactive mode
- ✅ Informational hooks (instructions file)
- ✅ Process detection
- ✅ Gas Town workflow integration

### Not Yet Supported
- ❌ Session resumption (no resume flag discovered)
- ❌ Session forking
- ❌ Executable lifecycle hooks
- ❌ Session ID environment variables

## Testing

### Unit Tests

```bash
# Test kiro package
cd internal/kiro && go test -v

# Test agent configuration
cd internal/config && go test -v -run Kiro
```

### Integration Testing

```bash
# Build the project
go build ./...

# Verify kiro agent is registered
go run ./cmd/gt config agents list | grep kiro
```

## Architecture Notes

### Design Decisions

1. **Informational Hooks**: Like Copilot, Kiro uses informational hooks (instructions file) rather than executable lifecycle hooks, since the kiro-cli interface doesn't appear to support executable hooks.

2. **Process Names**: Multiple process names are registered (`kiro-cli`, `kiro-cli-chat`, `zsh`) because Kiro sessions can appear under different process names depending on how they're launched.

3. **Session Management**: Session resume support is marked as unavailable for now, but can be added in the future if Kiro adds this capability.

4. **Ready Delay**: A 5-second ready delay is configured to allow Kiro to fully initialize before Gas Town sends commands.

### Comparison with Other Agents

**Similar to Copilot:**
- Informational hooks only
- No executable lifecycle hooks
- Instructions file for agent guidance

**Similar to Gemini:**
- CLI-based agent
- Supports non-interactive mode
- Config directory in `~/.kiro`

**Different from Claude:**
- No session environment variables
- No resume support (yet)
- No fork-session capability

## Future Enhancements

1. **Session Resumption**: If Kiro adds session resume capability, update:
   - `ResumeFlag` and `ResumeStyle` in preset
   - `SessionIDEnv` if using environment variables

2. **Executable Hooks**: If Kiro adds lifecycle hook support, convert to full hook integration like Claude/Gemini

3. **Auto-approval Mode**: Investigate if Kiro has equivalent to `--dangerously-skip-permissions` or if it needs environment variable configuration

4. **Process Name Refinement**: Monitor actual kiro process names in production and refine the `ProcessNames` list as needed

## Files Modified/Created

### Created
- `internal/kiro/settings.go`
- `internal/kiro/settings_test.go`
- `internal/kiro/plugin/gastown-instructions.md`
- `internal/config/agents_kiro_test.go`
- `docs/kiro-cli-integration.md`

### Modified
- `internal/config/agents.go` - Added AgentKiro constant and preset
- `internal/runtime/runtime.go` - Registered kiro hook installer

## References

- Main agent registry: `internal/config/agents.go`
- Runtime integration: `internal/runtime/runtime.go`
- Agent preset documentation: `docs/design/agent-provider-interface.md`
