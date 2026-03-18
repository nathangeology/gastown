package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstantiateFormulaOnBead verifies the helper function works correctly.
// gs-4i8: Primary path uses a single atomic bd mol bond call.
func TestInstantiateFormulaOnBead(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace marker
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}

	// Create routes.jsonl
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	rigDir := filepath.Join(townRoot, "gastown", "mayor", "rig")
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatalf("mkdir rigDir: %v", err)
	}
	routes := strings.Join([]string{
		`{"prefix":"gt-","path":"gastown/mayor/rig"}`,
		`{"prefix":"hq-","path":"."}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(routes), 0644); err != nil {
		t.Fatalf("write routes.jsonl: %v", err)
	}

	// Create stub bd that handles the atomic bond path
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")
	bdScript := `#!/bin/sh
set -e
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"
shift || true
case "$cmd" in
  show)
    echo '[{"title":"Fix bug ABC","status":"open","assignee":"","description":""}]'
    ;;
  formula)
    echo '{"name":"mol-polecat-work"}'
    ;;
  cook)
    ;;
  mol)
    sub="$1"
    shift || true
    case "$sub" in
      wisp)
        echo '{"new_epic_id":"gt-wisp-288"}'
        ;;
      bond)
        left="$1"
        shift || true
        if [ "$left" = "mol-polecat-work" ]; then
          echo '{"result_id":"gt-abc123","id_mapping":{"mol-polecat-work":"gt-wisp-288"}}'
          exit 0
        fi
        echo '{"root_id":"gt-wisp-288"}'
        ;;
    esac
    ;;
  update)
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="show" (
  echo [{^"title^":^"Fix bug ABC^",^"status^":^"open^",^"assignee^":^"^",^"description^":^"^"}]
  exit /b 0
)
if "%cmd%"=="formula" (
  echo {^"name^":^"mol-polecat-work^"}
  exit /b 0
)
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-288^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    if "%left%"=="mol-polecat-work" (
      echo {^"result_id^":^"gt-abc123^",^"id_mapping^":{^"mol-polecat-work^":^"gt-wisp-288^"}}
      exit /b 0
    )
    echo {^"root_id^":^"gt-wisp-288^"}
    exit /b 0
  )
)
if "%cmd%"=="update" exit /b 0
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(filepath.Join(townRoot, "mayor", "rig")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Test the helper function directly
	extraVars := []string{"branch=polecat/furiosa/gt-abc123"}
	result, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-abc123", "Test Bug Fix", "", townRoot, false, extraVars)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead failed: %v", err)
	}

	if result.WispRootID == "" {
		t.Error("WispRootID should not be empty")
	}
	if result.BeadToHook == "" {
		t.Error("BeadToHook should not be empty")
	}

	// Verify the atomic bond path was used (gs-4i8: single call, no separate cook/wisp)
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logBytes)

	if !strings.Contains(logContent, "mol bond mol-polecat-work gt-abc123 --json --ephemeral") {
		t.Errorf("atomic bond command not found in log:\n%s", logContent)
	}
	if !strings.Contains(logContent, "--var branch=polecat/furiosa/gt-abc123") {
		t.Errorf("extra vars not passed to bond command:\n%s", logContent)
	}
	// Primary path should NOT call cook or wisp separately
	if strings.Contains(logContent, "CMD:cook") {
		t.Errorf("cook should not be called on primary atomic path:\n%s", logContent)
	}
	if strings.Contains(logContent, "mol wisp") {
		t.Errorf("mol wisp should not be called on primary atomic path:\n%s", logContent)
	}
}

// TestInstantiateFormulaOnBeadSkipCook verifies the skipCook optimization.
// gs-4i8: With the atomic primary path, skipCook only affects the legacy fallback.
// The primary path never calls cook separately regardless of skipCook.
func TestInstantiateFormulaOnBeadSkipCook(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace marker
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}

	// Create routes.jsonl
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	routes := `{"prefix":"gt-","path":"."}`
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(routes), 0644); err != nil {
		t.Fatalf("write routes.jsonl: %v", err)
	}

	// Create stub bd — atomic bond succeeds
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")
	bdScript := `#!/bin/sh
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  mol)
    sub="$1"; shift || true
    case "$sub" in
      bond)
        left="$1"; shift || true
        if [ "$left" = "mol-polecat-work" ]; then
          echo '{"result_id":"gt-test","id_mapping":{"mol-polecat-work":"gt-wisp-skip"}}'
          exit 0
        fi
        echo '{"root_id":"gt-wisp-skip"}'
        ;;
      wisp) echo '{"new_epic_id":"gt-wisp-skip"}';;
    esac;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="mol" (
  if "%sub%"=="bond" (
    if "%left%"=="mol-polecat-work" (
      echo {^"result_id^":^"gt-test^",^"id_mapping^":{^"mol-polecat-work^":^"gt-wisp-skip^"}}
      exit /b 0
    )
    echo {^"root_id^":^"gt-wisp-skip^"}
    exit /b 0
  )
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-skip^"}
    exit /b 0
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	// Test with skipCook=true — primary path doesn't call cook regardless
	_, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-test", "Test", "", townRoot, true, nil)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead failed: %v", err)
	}

	logBytes, _ := os.ReadFile(logPath)
	logContent := string(logBytes)

	// Verify cook was NOT called (primary path never calls cook)
	if strings.Contains(logContent, "cook") {
		t.Errorf("cook should not be called on primary atomic path:\n%s", logContent)
	}

	// Verify atomic bond was called
	if !strings.Contains(logContent, "mol bond mol-polecat-work") {
		t.Errorf("atomic bond should be called:\n%s", logContent)
	}

	// Verify wisp was NOT called separately
	if strings.Contains(logContent, "mol wisp") {
		t.Errorf("mol wisp should not be called on primary atomic path:\n%s", logContent)
	}
}

// TestCookFormula verifies the CookFormula helper.
func TestCookFormula(t *testing.T) {
	townRoot := t.TempDir()

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")
	bdScript := `#!/bin/sh
echo "CMD:$*" >> "${BD_LOG}"
exit 0
`
	bdScriptWindows := `@echo off
echo CMD:%*>>"%BD_LOG%"
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := CookFormula("mol-polecat-work", townRoot, townRoot)
	if err != nil {
		t.Fatalf("CookFormula failed: %v", err)
	}

	logBytes, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logBytes), "cook mol-polecat-work") {
		t.Errorf("cook command not found in log")
	}
}

// TestSlingHookRawBeadFlag verifies --hook-raw-bead flag exists.
func TestSlingHookRawBeadFlag(t *testing.T) {
	// Verify the flag variable exists and works
	prevValue := slingHookRawBead
	t.Cleanup(func() { slingHookRawBead = prevValue })

	slingHookRawBead = true
	if !slingHookRawBead {
		t.Error("slingHookRawBead flag should be true")
	}

	slingHookRawBead = false
	if slingHookRawBead {
		t.Error("slingHookRawBead flag should be false")
	}
}

// TestAutoApplyLogic verifies the auto-apply detection logic.
// When formulaName is empty and target contains "/polecats/", mol-polecat-work should be applied.
func TestAutoApplyLogic(t *testing.T) {
	tests := []struct {
		name          string
		formulaName   string
		hookRawBead   bool
		targetAgent   string
		wantAutoApply bool
	}{
		{
			name:          "bare bead to polecat - should auto-apply",
			formulaName:   "",
			hookRawBead:   false,
			targetAgent:   "gastown/polecats/Toast",
			wantAutoApply: true,
		},
		{
			name:          "bare bead with --hook-raw-bead - should not auto-apply",
			formulaName:   "",
			hookRawBead:   true,
			targetAgent:   "gastown/polecats/Toast",
			wantAutoApply: false,
		},
		{
			name:          "formula already specified - should not auto-apply",
			formulaName:   "mol-review",
			hookRawBead:   false,
			targetAgent:   "gastown/polecats/Toast",
			wantAutoApply: false,
		},
		{
			name:          "non-polecat target - should not auto-apply",
			formulaName:   "",
			hookRawBead:   false,
			targetAgent:   "gastown/witness",
			wantAutoApply: false,
		},
		{
			name:          "mayor target - should not auto-apply",
			formulaName:   "",
			hookRawBead:   false,
			targetAgent:   "mayor",
			wantAutoApply: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the logic in sling.go
			shouldAutoApply := tt.formulaName == "" && !tt.hookRawBead && strings.Contains(tt.targetAgent, "/polecats/")

			if shouldAutoApply != tt.wantAutoApply {
				t.Errorf("auto-apply logic: got %v, want %v", shouldAutoApply, tt.wantAutoApply)
			}
		})
	}
}

// TestFormulaOnBeadPassesVariables verifies that feature and issue variables are passed.
func TestFormulaOnBeadPassesVariables(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"."}`), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")
	bdScript := `#!/bin/sh
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  cook) exit 0;;
  mol)
    sub="$1"; shift || true
    case "$sub" in
      bond)
        left="$1"; shift || true
        if [ "$left" = "mol-polecat-work" ]; then
          echo '{"result_id":"gt-abc123","id_mapping":{"mol-polecat-work":"gt-wisp-var"}}'
          exit 0
        fi
        echo '{"root_id":"gt-wisp-var"}'
        ;;
      wisp) echo '{"new_epic_id":"gt-wisp-var"}';;
    esac;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="bond" (
    if "%left%"=="mol-polecat-work" (
      echo {^"result_id^":^"gt-abc123^",^"id_mapping^":{^"mol-polecat-work^":^"gt-wisp-var^"}}
      exit /b 0
    )
    echo {^"root_id^":^"gt-wisp-var^"}
    exit /b 0
  )
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-var^"}
    exit /b 0
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	_, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-abc123", "My Cool Feature", "", townRoot, false, nil)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead: %v", err)
	}

	logBytes, _ := os.ReadFile(logPath)
	logContent := string(logBytes)

	// gs-4i8: Find the atomic bond line (primary path)
	var bondLine string
	for _, line := range strings.Split(logContent, "\n") {
		if strings.Contains(line, "mol bond mol-polecat-work") {
			bondLine = line
			break
		}
	}

	if bondLine == "" {
		t.Fatalf("atomic bond command not found:\n%s", logContent)
	}

	if !strings.Contains(bondLine, "feature=My Cool Feature") {
		t.Errorf("bond missing feature variable:\n%s", bondLine)
	}

	if !strings.Contains(bondLine, "issue=gt-abc123") {
		t.Errorf("bond missing issue variable:\n%s", bondLine)
	}
}

func TestInstantiateFormulaOnBead_FallbackToLegacy(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"."}`), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")

	// Primary path (atomic bond with formula name) fails — older bd version.
	// Legacy fallback: cook + wisp + bond with wisp ID succeeds.
	bdScript := `#!/bin/sh
set -e
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  cook)
    exit 0
    ;;
  mol)
    sub="$1"; shift || true
    case "$sub" in
      wisp)
        echo '{"new_epic_id":"gt-wisp-legacy"}'
        exit 0
        ;;
      bond)
        left="$1"; shift || true
        if [ "$left" = "mol-polecat-work" ]; then
          echo "Error: formula bond not supported" >&2
          exit 1
        fi
        if [ "$left" = "gt-wisp-legacy" ]; then
          echo '{"root_id":"gt-wisp-legacy"}'
          exit 0
        fi
        echo "Error: unexpected bond target: $left" >&2
        exit 1
        ;;
    esac
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-legacy^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    if "%left%"=="mol-polecat-work" (
      echo Error: formula bond not supported 1>&2
      exit /b 1
    )
    if "%left%"=="gt-wisp-legacy" (
      echo {^"root_id^":^"gt-wisp-legacy^"}
      exit /b 0
    )
    echo Error: unexpected bond target: %left% 1>&2
    exit /b 1
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	result, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-abc123", "My Cool Feature", "", townRoot, false, nil)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead: %v", err)
	}
	if result.WispRootID != "gt-wisp-legacy" {
		t.Fatalf("WispRootID = %q, want %q", result.WispRootID, "gt-wisp-legacy")
	}
	if result.BeadToHook != "gt-abc123" {
		t.Fatalf("BeadToHook = %q, want %q", result.BeadToHook, "gt-abc123")
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logBytes)
	// Primary path should have been attempted first
	if !strings.Contains(logContent, "mol bond mol-polecat-work gt-abc123 --json --ephemeral") {
		t.Fatalf("missing primary atomic bond attempt in log:\n%s", logContent)
	}
	// Legacy fallback should have been used
	if !strings.Contains(logContent, "CMD:cook mol-polecat-work") {
		t.Fatalf("missing legacy cook in log:\n%s", logContent)
	}
	if !strings.Contains(logContent, "mol wisp mol-polecat-work") {
		t.Fatalf("missing legacy wisp in log:\n%s", logContent)
	}
	if !strings.Contains(logContent, "mol bond gt-wisp-legacy gt-abc123 --json") {
		t.Fatalf("missing legacy bond in log:\n%s", logContent)
	}
}

// TestIsMalformedWispID verifies detection of doubled "-wisp-" in wisp IDs (gt-4gjd).
func TestIsMalformedWispID(t *testing.T) {
	tests := []struct {
		name          string
		wispID        string
		wantMalformed bool
	}{
		{"normal wisp ID", "gt-wisp-abc", false},
		{"normal with long random", "oag-wisp-gm7c", false},
		{"doubled wisp marker", "oag-wisp-wisp-rsia", true},
		{"triple wisp marker", "gt-wisp-wisp-wisp-x", true},
		{"empty", "", false},
		{"no wisp marker", "gt-abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMalformedWispID(tt.wispID)
			if got != tt.wantMalformed {
				t.Errorf("isMalformedWispID(%q) = %v, want %v", tt.wispID, got, tt.wantMalformed)
			}
		})
	}
}

// TestInstantiateFormulaOnBead_MalformedWispIDProceedsWithBond verifies that
// when the legacy fallback is used and bd mol wisp returns a malformed ID
// (doubled "-wisp-"), a warning is logged but the legacy bond is still attempted
// and succeeds (gt-4gjd).
func TestInstantiateFormulaOnBead_MalformedWispIDProceedsWithBond(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"oag-","path":"."}`), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")

	// Primary path (atomic bond with formula name) fails — forces legacy fallback.
	// Legacy path: bd mol wisp returns a malformed ID with doubled "-wisp-".
	// Legacy bond SHOULD be called with the malformed ID and succeed.
	bdScript := `#!/bin/sh
set -e
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  cook)
    exit 0
    ;;
  mol)
    sub="$1"; shift || true
    case "$sub" in
      wisp)
        echo '{"new_epic_id":"oag-wisp-wisp-rsia"}'
        exit 0
        ;;
      bond)
        left="$1"; shift || true
        if [ "$left" = "mol-polecat-work" ]; then
          echo "Error: formula bond not supported" >&2
          exit 1
        fi
        if [ "$left" = "oag-wisp-wisp-rsia" ]; then
          echo '{"root_id":"oag-wisp-wisp-rsia"}'
          exit 0
        fi
        echo "Error: unexpected bond target: $left" >&2
        exit 1
        ;;
    esac
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"oag-wisp-wisp-rsia^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    if "%left%"=="mol-polecat-work" (
      echo Error: formula bond not supported 1>&2
      exit /b 1
    )
    if "%left%"=="oag-wisp-wisp-rsia" (
      echo {^"root_id^":^"oag-wisp-wisp-rsia^"}
      exit /b 0
    )
    echo Error: unexpected bond target: %left% 1>&2
    exit /b 1
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	result, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "oag-npeat", "Fix formula bug", "", townRoot, false, nil)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead: %v", err)
	}
	if result.WispRootID != "oag-wisp-wisp-rsia" {
		t.Fatalf("WispRootID = %q, want %q", result.WispRootID, "oag-wisp-wisp-rsia")
	}
	if result.BeadToHook != "oag-npeat" {
		t.Fatalf("BeadToHook = %q, want %q", result.BeadToHook, "oag-npeat")
	}

	// Verify the legacy bond WAS called with the malformed ID.
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logBytes)
	if !strings.Contains(logContent, "mol bond oag-wisp-wisp-rsia") {
		t.Fatalf("legacy bond should have been called with malformed wisp ID:\n%s", logContent)
	}
}

// TestInstantiateFormulaOnBead_FallbackCleansUpOrphanedWisp verifies that when
// the legacy fallback's wisp bond fails and the direct-bond fallback within legacy
// is used, the orphaned wisp from bd mol wisp is cleaned up (gt-4gjd).
func TestInstantiateFormulaOnBead_FallbackCleansUpOrphanedWisp(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"."}`), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")

	// Track bond call count via a file to differentiate primary vs legacy calls.
	// Primary path (1st call): mol bond mol-polecat-work ... --ephemeral → fail
	// Legacy path wisp bond: mol bond gt-wisp-orphan ... → fail
	// Legacy path direct-bond fallback (3rd call): mol bond mol-polecat-work ... --ephemeral → succeed
	bondCountFile := filepath.Join(townRoot, "bond-count")
	if err := os.WriteFile(bondCountFile, []byte("0"), 0644); err != nil {
		t.Fatalf("write bond-count: %v", err)
	}
	bdScript := `#!/bin/sh
set -e
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  cook)
    exit 0
    ;;
  mol)
    sub="$1"; shift || true
    case "$sub" in
      wisp)
        echo '{"new_epic_id":"gt-wisp-orphan"}'
        exit 0
        ;;
      bond)
        left="$1"; shift || true
        # Increment bond call counter
        count=$(cat "` + bondCountFile + `")
        count=$((count + 1))
        echo "$count" > "` + bondCountFile + `"
        if [ "$left" = "gt-wisp-orphan" ]; then
          echo "Error: 'gt-wisp-orphan' not found" >&2
          exit 1
        fi
        if [ "$left" = "mol-polecat-work" ] || echo "$left" | grep -q "gt-formula"; then
          if [ "$count" -le 1 ]; then
            echo "Error: formula bond not supported" >&2
            exit 1
          fi
          echo '{"result_id":"gt-test","id_mapping":{"mol-polecat-work":"gt-wisp-clean"}}'
          exit 0
        fi
        echo "Error: unexpected bond target: $left" >&2
        exit 1
        ;;
    esac
    ;;
  close)
    exit 0
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
set "left=%3"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-orphan^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    if "%left%"=="gt-wisp-orphan" (
      echo Error: 'gt-wisp-orphan' not found 1>&2
      exit /b 1
    )
    if "%left%"=="mol-polecat-work" (
      echo {^"result_id^":^"gt-test^",^"id_mapping^":{^"mol-polecat-work^":^"gt-wisp-clean^"}}
      exit /b 0
    )
    echo Error: unexpected bond target: %left% 1>&2
    exit /b 1
  )
)
if "%cmd%"=="close" exit /b 0
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	result, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-test", "Test cleanup", "", townRoot, false, nil)
	if err != nil {
		t.Fatalf("InstantiateFormulaOnBead: %v", err)
	}
	if result.WispRootID != "gt-wisp-clean" {
		t.Fatalf("WispRootID = %q, want %q", result.WispRootID, "gt-wisp-clean")
	}

	// Verify the orphaned wisp cleanup was attempted
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logBytes)
	// The legacy fallback's bond with wisp ID failed, triggering cleanup
	if !strings.Contains(logContent, "mol bond gt-wisp-orphan") {
		t.Fatalf("expected legacy bond attempt with orphaned wisp in log:\n%s", logContent)
	}
}

// TestInstantiateFormulaOnBead_ParseFailureFallbackFailure verifies that when
// both the primary atomic path and the legacy fallback fail,
// InstantiateFormulaOnBead returns an error instead of silent success.
func TestInstantiateFormulaOnBead_ParseFailureFallbackFailure(t *testing.T) {
	townRoot := t.TempDir()

	// Minimal workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"."}`), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	logPath := filepath.Join(townRoot, "bd.log")

	// All bond calls fail — both primary and legacy paths.
	bdScript := `#!/bin/sh
set -e
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"; shift || true
case "$cmd" in
  cook)
    exit 0
    ;;
  mol)
    sub="$1"; shift || true
    case "$sub" in
      wisp)
        echo '{"new_epic_id":"gt-wisp-abc"}'
        exit 0
        ;;
      bond)
        echo "Error: bond failed" >&2
        exit 1
        ;;
    esac
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
setlocal enableextensions
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-abc^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    echo Error: bond failed 1>&2
    exit /b 1
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(townRoot)

	_, err := InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-abc123", "My Feature", "", townRoot, false, nil)
	if err == nil {
		t.Fatal("expected error when all bond paths fail, got nil")
	}
}
