package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// Store represents the .psyduck/ workspace directory: the single source of
// truth for where plugin binaries live, where the manifest is written, and
// how plugins are loaded at run time.
type Store struct {
	root string
}

func NewStore(root string) *Store {
	return &Store{root: root}
}

func (s *Store) pluginsDir() string {
	return filepath.Join(s.root, "plugins")
}

func (s *Store) manifestPath() string {
	return filepath.Join(s.root, "plugin.json")
}

func (s *Store) soPath(name string) string {
	return filepath.Join(s.pluginsDir(), name+".so")
}

func (s *Store) readManifest() (map[string]string, error) {
	data, err := os.ReadFile(s.manifestPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to read plugin manifest: %w", err)
	}

	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to decode plugin manifest: %w", err)
	}
	return m, nil
}

func (s *Store) writeManifest(m map[string]string) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(s.manifestPath(), b, 0o644)
}

// Build clones and compiles the declared plugins, writing the name → .so path
// manifest to the store's manifest file. Used by the init command.
func (s *Store) Build(specs []parse.Plugin) error {
	if err := os.MkdirAll(s.pluginsDir(), os.ModeDir|os.ModePerm); err != nil {
		return fmt.Errorf("failed to create plugins dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "psyduck-plugin-*")
	if err != nil {
		return fmt.Errorf("failed to make temp dir: %w", err)
	}
	f := &fetcher{store: s, tmpDir: tmpDir}
	defer f.cleanup()

	collected := make(map[string]string, len(specs))
	for _, spec := range specs {
		loc, err := f.fetch(spec)
		if err != nil {
			return fmt.Errorf("unable to fetch %s: %w", spec.Name, err)
		}
		collected[spec.Name] = loc
	}
	return s.writeManifest(collected)
}

// Load reads the manifest and opens every plugin listed in it.
// A missing manifest is not an error — it means no plugins have been built,
// which is valid for stdlib-only pipelines.
func (s *Store) Load() ([]sdk.Plugin, error) {
	manifest, err := s.readManifest()
	if err != nil {
		return nil, err
	}

	loaded := make([]sdk.Plugin, 0, len(manifest))
	for name, soPath := range manifest {
		p, err := loadBinary(name, soPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin %s: %w", name, err)
		}
		loaded = append(loaded, p)
	}
	return loaded, nil
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
