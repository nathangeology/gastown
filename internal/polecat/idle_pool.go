package polecat

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// idlePoolFile is the filename within the polecats/ directory that caches
// idle polecat names, one per line. This gives FindIdlePolecat a zero-Dolt-query
// fast path for the common case where idle polecats are available.
const idlePoolFile = ".idle-pool"

// idlePoolPath returns the path to the idle-pool file for this rig.
func (m *Manager) idlePoolPath() string {
	return filepath.Join(m.rig.Path, "polecats", idlePoolFile)
}

// lockIdlePool acquires an exclusive file lock for idle-pool operations.
func (m *Manager) lockIdlePool() (*flock.Flock, error) {
	lockDir := filepath.Join(m.rig.Path, ".runtime", "locks")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, err
	}
	fl := flock.New(filepath.Join(lockDir, "idle-pool.lock"))
	if err := fl.Lock(); err != nil {
		return nil, err
	}
	return fl, nil
}

// WriteIdlePool appends a polecat name to the idle-pool file.
// Called by gt done when a polecat transitions to IDLE.
func (m *Manager) WriteIdlePool(name string) error {
	fl, err := m.lockIdlePool()
	if err != nil {
		return err
	}
	defer fl.Unlock()

	poolPath := m.idlePoolPath()
	if err := os.MkdirAll(filepath.Dir(poolPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(poolPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(name + "\n")
	return err
}

// PopIdlePool removes and returns the first name from the idle-pool file.
// Returns "" if the file is empty or doesn't exist.
func (m *Manager) PopIdlePool() string {
	fl, err := m.lockIdlePool()
	if err != nil {
		return ""
	}
	defer fl.Unlock()

	poolPath := m.idlePoolPath()
	data, err := os.ReadFile(poolPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for len(lines) > 0 {
		name := strings.TrimSpace(lines[0])
		lines = lines[1:]
		if name == "" {
			continue
		}
		// Validate: polecat directory must still exist
		if !m.exists(name) {
			continue
		}
		// Write remaining lines back
		remaining := strings.Join(lines, "\n")
		if remaining != "" {
			remaining += "\n"
		}
		_ = os.WriteFile(poolPath, []byte(remaining), 0644)
		return name
	}

	// All entries consumed or invalid — truncate
	_ = os.Remove(poolPath)
	return ""
}
