package kiro

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSettingsAt(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "kiro-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test creating settings file
	err = EnsureSettingsAt(tmpDir, ".kiro", "kiro-instructions.md")
	if err != nil {
		t.Fatalf("EnsureSettingsAt failed: %v", err)
	}

	// Verify the file was created
	settingsPath := filepath.Join(tmpDir, ".kiro", "kiro-instructions.md")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatalf("settings file was not created at %s", settingsPath)
	}

	// Read and verify content
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("settings file is empty")
	}

	// Test that it doesn't overwrite existing file
	err = EnsureSettingsAt(tmpDir, ".kiro", "kiro-instructions.md")
	if err != nil {
		t.Fatalf("EnsureSettingsAt failed on second call: %v", err)
	}

	// Verify content is unchanged
	newContent, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings file after second call: %v", err)
	}

	if string(content) != string(newContent) {
		t.Fatal("settings file was modified on second call")
	}
}

func TestEnsureSettingsAt_EmptyParams(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kiro-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with empty hooksDir - should return early without error
	err = EnsureSettingsAt(tmpDir, "", "kiro-instructions.md")
	if err != nil {
		t.Fatalf("EnsureSettingsAt with empty hooksDir should not error: %v", err)
	}

	// Test with empty hooksFile - should return early without error
	err = EnsureSettingsAt(tmpDir, ".kiro", "")
	if err != nil {
		t.Fatalf("EnsureSettingsAt with empty hooksFile should not error: %v", err)
	}
}
