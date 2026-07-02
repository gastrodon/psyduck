package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/psyduck-etl/sdk"
)

// GoPluginLoader loads plugins compiled with -buildmode=plugin, resolving
// .so paths from <initPath>/plugin.json (written by Fetch).
type GoPluginLoader struct {
	initPath string

	binPaths map[string]string
}

func NewGoPluginLoader(initPath string) *GoPluginLoader {
	return &GoPluginLoader{initPath: initPath}
}

// manifest reads plugin.json, caching the result. A missing manifest is
// not an error — it just means no plugins have been fetched, which is
// valid for stdlib-only pipelines.
func (l *GoPluginLoader) manifest() (map[string]string, error) {
	if l.binPaths != nil {
		return l.binPaths, nil
	}

	data, err := os.ReadFile(filepath.Join(l.initPath, "plugin.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.binPaths = map[string]string{}
			return l.binPaths, nil
		}
		return nil, fmt.Errorf("failed to read plugin manifest: %w", err)
	}

	binPaths := make(map[string]string)
	if err := json.Unmarshal(data, &binPaths); err != nil {
		return nil, fmt.Errorf("failed to decode plugin manifest: %w", err)
	}

	l.binPaths = binPaths
	return binPaths, nil
}

// LoadAll loads every plugin listed in the manifest.
func (l *GoPluginLoader) LoadAll() ([]sdk.Plugin, error) {
	manifest, err := l.manifest()
	if err != nil {
		return nil, err
	}

	loaded := make([]sdk.Plugin, 0, len(manifest))
	for name, soPath := range manifest {
		p, err := LoadBinary(name, soPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin %s: %w", name, err)
		}
		loaded = append(loaded, p)
	}
	return loaded, nil
}

// LoadBinary opens the shared object at soPath and returns the sdk.Plugin
// from its exported `func Plugin() sdk.Plugin` symbol.
func LoadBinary(name, soPath string) (sdk.Plugin, error) {
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
