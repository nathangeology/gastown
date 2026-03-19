package polecat

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// idlePoolFile is the filename within the polecats/ directory that caches
// known-idle polecat names, one per line. This provides a zero-Dolt-query
// fast path for FindIdlePolecat: pop a name from the file instead of
// scanning tmux sessions and querying beads.
const idlePoolFile = ".idle-pool"

// idlePoolPath returns the absolute path to the idle-pool file for a rig.
func idlePoolPath(rigPath string) string {
	return filepath.Join(rigPath, "polecats", idlePoolFile)
}

// idlePoolLockPath returns the flock path protecting the idle-pool file.
func idlePoolLockPath(rigPath string) string {
	return filepath.Join(rigPath, ".runtime", "locks", "idle-pool.lock")
}

// lockIdlePool acquires an exclusive file lock for idle-pool operations.
func lockIdlePool(rigPath string) (*flock.Flock, error) {
	lockDir := filepath.Join(rigPath, ".runtime", "locks")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, err
	}
	fl := flock.New(idlePoolLockPath(rigPath))
	if err := fl.Lock(); err != nil {
		return nil, err
	}
	return fl, nil
}

// AppendIdlePool adds a polecat name to the idle-pool file (called by gt done).
func AppendIdlePool(rigPath, name string) error {
	fl, err := lockIdlePool(rigPath)
	if err != nil {
		return err
	}
	defer fl.Unlock()

	poolPath := idlePoolPath(rigPath)
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
// Returns "" if the file is empty or missing.
func PopIdlePool(rigPath string) (string, error) {
	fl, err := lockIdlePool(rigPath)
	if err != nil {
		return "", err
	}
	defer fl.Unlock()

	poolPath := idlePoolPath(rigPath)
	data, err := os.ReadFile(poolPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Filter empty lines
	var names []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	if len(names) == 0 {
		return "", nil
	}

	popped := names[0]
	remaining := names[1:]

	if len(remaining) == 0 {
		_ = os.Remove(poolPath)
	} else {
		_ = os.WriteFile(poolPath, []byte(strings.Join(remaining, "\n")+"\n"), 0644)
	}

	return popped, nil
}
