package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/rpc"

	"github.com/gastrodon/psyduck/parse"
)

// Store represents the .psyduck/ directory: on-disk storage for built
// plugin binaries. Each binary is written to `<plugin>-psyduck-<sha7>`,
// where the 7-char sha is a prefix of the sha256 over the file's bytes.
// The short-hash suffix makes plugin subprocesses recognizable in `ps` /
// `top` while still tying each file to specific bytes; the full sha256
// stays in the lock file (see lock.go) and is what Load verifies against.
// The store itself is stateless — the same store can back any number of
// lock files.
type Store struct {
	root string

	// clients tracks the plugin subprocesses Load has launched, so Close
	// can tear them down with the store instead of leaking them.
	clients []*rpc.Client
}

func NewStore(root string) *Store {
	// The root must be absolute: relative paths would silently misbehave in
	// fetcher.build (`go build -C` resolves -o relative to the -C directory,
	// not our cwd).
	abs, err := filepath.Abs(root)
	if err != nil {
		// Abs only fails if the cwd is undeterminable; keep the given root.
		abs = root
	}
	return &Store{root: abs}
}

func (s *Store) pluginsDir() string {
	return filepath.Join(s.root, "plugins")
}

// binPath is the on-disk location for a plugin's binary: one file per
// plugin name, suffixed with the first 7 hex chars of its full sha256 so
// running processes are easy to pick out in `ps` / `top`. The suffix is
// cosmetic — the full hash in the lock is what Load verifies against the
// file's bytes — so a malformed short hash (truncated/hand-edited lock)
// just uses whatever's there rather than panicking; verifyHash then
// rejects it with a clean error.
func (s *Store) binPath(name, hash string) string {
	suffix := hash
	if len(hash) > 7 {
		suffix = hash[:7]
	}
	return filepath.Join(s.pluginsDir(), name+"-psyduck-"+suffix)
}

// storeBinary writes the file at path to `<name>-psyduck-<sha7>`, where
// the sha is computed from the file's bytes. It always writes, even if a
// file already exists at that path — the expensive work (build or clone)
// already happened by the time storeBinary is called, so skipping the
// write would only save one cheap copy at the cost of never self-healing
// a slot whose on-disk content has drifted (e.g. been tampered with, or
// corrupted) from what its filename promises. Returns the full sha256
// hex, which is what the lock records.
func (s *Store) storeBinary(path, name string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read plugin binary: %w", err)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dest := s.binPath(name, hash)

	if err := os.MkdirAll(s.pluginsDir(), os.ModeDir|os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create plugins dir: %w", err)
	}
	// 0o755: plugins are executables now, launched as subprocesses at Load.
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		return "", fmt.Errorf("failed to store plugin binary: %w", err)
	}
	return hash, nil
}

// Build clones and compiles every declared plugin, content-addressing
// each resulting binary into the store. It returns the lock data for the
// caller to write next to the source file being init'd — the store
// itself holds no name-based state, so nothing here depends on which
// file the specs came from.
func (s *Store) Build(specs []parse.Plugin) (map[string]LockedPlugin, error) {
	tmpDir, err := os.MkdirTemp("", "psyduck-plugin-*")
	if err != nil {
		return nil, fmt.Errorf("failed to make temp dir: %w", err)
	}
	f := &fetcher{store: s, tmpDir: tmpDir}
	defer f.cleanup()

	locked := make(map[string]LockedPlugin, len(specs))
	for _, spec := range specs {
		hash, resolve, err := f.fetch(spec)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch %s: %w", spec.Name, err)
		}
		locked[spec.Name] = LockedPlugin{Source: spec.Source, Ref: resolve, Hash: hash}
	}
	return locked, nil
}

// Load launches every plugin recorded in locked as a subprocess (see
// sdk/rpc), verifying each binary's content still matches its locked hash
// before launching it — catching a store that's missing, corrupted, or
// drifted out of sync with the lock file it's supposed to satisfy.
//
// The subprocesses stay alive behind the returned sdk.Plugins until Close;
// a partial failure tears down whatever was already launched.
func (s *Store) Load(locked map[string]LockedPlugin) ([]sdk.Plugin, error) {
	loaded := make([]sdk.Plugin, 0, len(locked))
	for name, entry := range locked {
		binPath := s.binPath(name, entry.Hash)
		if err := verifyHash(binPath, entry.Hash); err != nil {
			s.Close()
			return nil, fmt.Errorf("plugin %s: %w", name, err)
		}
		client, err := rpc.Dial(binPath, pluginLogger(name))
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("failed to load plugin %s: %w", name, err)
		}
		s.clients = append(s.clients, client)
		loaded = append(loaded, client.Plugin)
	}
	return loaded, nil
}

// Close tears down every plugin subprocess this store has launched,
// invalidating the sdk.Plugins Load returned. Safe to call more than once.
func (s *Store) Close() {
	for _, c := range s.clients {
		c.Kill()
	}
	s.clients = nil
}

// CleanupClients kills every plugin subprocess launched in this process,
// regardless of which store launched it. Hosts call it once on the way
// out so no plugin outlives its run.
func CleanupClients() { rpc.CleanupClients() }

// pluginLogger builds the logger plugin-subprocess machinery (launch,
// handshake, forwarded plugin stderr) logs through. It honors
// PSYDUCK_LOG_LEVEL like core's pipeline logger, but defaults to Warn
// rather than Info: go-plugin's Info output is per-run lifecycle noise
// (started/exited lines), not something a pipeline user asked for.
func pluginLogger(name string) hclog.Logger {
	level := hclog.Warn
	switch os.Getenv("PSYDUCK_LOG_LEVEL") {
	case "trace":
		level = hclog.Trace
	case "debug":
		level = hclog.Debug
	case "error", "fatal", "panic":
		level = hclog.Error
	}
	return hclog.New(&hclog.LoggerOptions{
		Name:   "plugin." + name,
		Level:  level,
		Output: os.Stderr,
	})
}

// verifyHash confirms the file at path still hashes to want.
func verifyHash(path, want string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("binary missing at %s (run init again): %w", path, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("hash mismatch at %s: locked %s, found %s (run init again)", path, want, got)
	}
	return nil
}
