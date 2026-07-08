package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LockedPlugin is one plugin's resolved, content-addressed entry in a
// lock file: where it came from, the git ref that was actually checked
// out (empty for local, non-git sources), and the hash of the exact .so
// bytes that were built from it.
//
// Resolve is always the *actual* ref init resolved at build time — a
// branch (refs/heads/<name>), a tag (refs/tags/<name>), or, if neither
// applies, the commit's full SHA — never just an echo of whatever the
// plugin{} block's optional `tag` attribute said. That's true whether or
// not `tag` was set: an unset tag still checks out (and records) whatever
// the default branch resolved to at init time.
type LockedPlugin struct {
	Source  string `json:"source"`
	Resolve string `json:"resolve,omitempty"`
	Hash    string `json:"hash"`
}

// Lock is the full contents of a <file>.lock: every plugin (deduplicated
// by name) reachable from that file's import closure.
type Lock struct {
	Plugins map[string]LockedPlugin `json:"plugins"`
}

// LockPath returns the lock file path for a .psy file: path/to/name.psy ->
// path/to/name.lock, sitting next to the source file it locks.
func LockPath(file string) string {
	return strings.TrimSuffix(file, ".psy") + ".lock"
}

// ReadLock reads the lock file for file. A missing lock file is reported
// as a specific, actionable error rather than treated as "no plugins" —
// every file that's run must have been explicitly init'd first.
func ReadLock(file string) (*Lock, error) {
	path := LockPath(file)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: no lock file — run `psyduck init %s` first", file, file)
		}
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	lock := &Lock{}
	if err := json.Unmarshal(data, lock); err != nil {
		return nil, fmt.Errorf("%s: malformed lock file: %w", path, err)
	}
	if lock.Plugins == nil {
		lock.Plugins = map[string]LockedPlugin{}
	}
	return lock, nil
}

// WriteLock writes the lock file for file, formatted for readability since
// lock files are meant to be committed and diffed.
func WriteLock(file string, lock *Lock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(LockPath(file), data, 0o644)
}
