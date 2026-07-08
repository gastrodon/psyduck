package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// Store represents the .psyduck/ directory: content-addressed storage for
// built plugin binaries, keyed by the sha256 of their own bytes. The
// mapping from plugin name to hash lives in a separate per-file lock file
// (see lock.go) — the store itself only knows how to place and retrieve
// binaries by hash, so the same store can back any number of lock files.
type Store struct {
	root string
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

func (s *Store) hashPath(hash string) string {
	return filepath.Join(s.pluginsDir(), hash+".so")
}

// storeBinary content-addresses the file at path into the store: hashes
// its bytes and writes them to <hash>.so. It always writes, even if a
// file already exists at that hash path — the expensive work (build or
// clone) already happened by the time storeBinary is called, so skipping
// the write would only save one cheap copy at the cost of never
// self-healing a hash slot whose on-disk content has drifted (e.g. been
// tampered with, or corrupted) from what its filename promises. Two specs
// that build to identical bytes still dedupe, since they land on the same
// hash path with the same (correct) content either way. Returns the hash.
func (s *Store) storeBinary(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read plugin binary: %w", err)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dest := s.hashPath(hash)

	if err := os.MkdirAll(s.pluginsDir(), os.ModeDir|os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create plugins dir: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
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
		locked[spec.Name] = LockedPlugin{Source: spec.Source, Resolve: resolve, Hash: hash}
	}
	return locked, nil
}

// Load opens every plugin recorded in locked, verifying each binary's
// content still matches its locked hash before opening it — catching a
// store that's missing, corrupted, or drifted out of sync with the lock
// file it's supposed to satisfy.
func (s *Store) Load(locked map[string]LockedPlugin) ([]sdk.Plugin, error) {
	loaded := make([]sdk.Plugin, 0, len(locked))
	for name, entry := range locked {
		soPath := s.hashPath(entry.Hash)
		if err := verifyHash(soPath, entry.Hash); err != nil {
			return nil, fmt.Errorf("plugin %s: %w", name, err)
		}
		p, err := loadBinary(name, soPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin %s: %w", name, err)
		}
		loaded = append(loaded, p)
	}
	return loaded, nil
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

// loadBinary opens the shared object at soPath and returns the sdk.Plugin
// from its exported `func Plugin() sdk.Plugin` symbol.
func loadBinary(name, soPath string) (sdk.Plugin, error) {
	p, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin %s at %s: %w", name, soPath, err)
	}

	sym, err := p.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup Plugin symbol for %s: %w", name, err)
	}

	makePlugin, ok := sym.(func() sdk.Plugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s: Plugin symbol is not func() sdk.Plugin: %T", name, sym)
	}

	return makePlugin(), nil
}
