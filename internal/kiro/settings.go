// Package kiro provides Kiro CLI configuration management.
package kiro

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed plugin/gastown-instructions.md
var pluginFS embed.FS

// EnsureSettingsAt ensures the Gas Town custom instructions file exists for Kiro.
// If the file already exists, it's left unchanged.
// workDir is the agent's working directory where instructions are provisioned.
// hooksDir is the directory within workDir (e.g., ".kiro").
// hooksFile is the filename (e.g., "kiro-instructions.md").
func EnsureSettingsAt(workDir, hooksDir, hooksFile string) error {
	if hooksDir == "" || hooksFile == "" {
		return nil
	}

	settingsPath := filepath.Join(workDir, hooksDir, hooksFile)
	if _, err := os.Stat(settingsPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking kiro instructions file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("creating kiro settings directory: %w", err)
	}

	content, err := pluginFS.ReadFile("plugin/gastown-instructions.md")
	if err != nil {
		return fmt.Errorf("reading kiro instructions template: %w", err)
	}

	if err := os.WriteFile(settingsPath, content, 0644); err != nil {
		return fmt.Errorf("writing kiro instructions: %w", err)
	}

	return nil
}
