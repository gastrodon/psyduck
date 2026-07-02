package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// Loader turns a plugin declaration into a live sdk.Plugin. The go/plugin
// dlopen model is one implementation; socket/RPC loaders can implement
// this later without touching callers.
type Loader interface {
	Load(spec parse.PluginSpec) (sdk.Plugin, error)
}

// GoPluginLoader loads plugins compiled with -buildmode=plugin, resolving
// .so paths from <initPath>/plugin.json (written by FetchPlugins).
type GoPluginLoader struct {
	initPath string

	binPaths map[string]string
}

func NewGoPluginLoader(initPath string) *GoPluginLoader {
	return &GoPluginLoader{initPath: initPath}
}

func (l *GoPluginLoader) manifest() (map[string]string, error) {
	if l.binPaths != nil {
		return l.binPaths, nil
	}

	data, err := os.ReadFile(filepath.Join(l.initPath, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin manifest: %w", err)
	}

	binPaths := make(map[string]string)
	if err := json.Unmarshal(data, &binPaths); err != nil {
		return nil, fmt.Errorf("failed to decode plugin manifest: %w", err)
	}

	l.binPaths = binPaths
	return binPaths, nil
}

func (l *GoPluginLoader) Load(spec parse.PluginSpec) (sdk.Plugin, error) {
	binPaths, err := l.manifest()
	if err != nil {
		return nil, err
	}

	soPath, ok := binPaths[spec.Name]
	if !ok {
		return nil, fmt.Errorf("no binary for plugin %s — run `psyduck init`?", spec.Name)
	}

	return LoadBinary(spec.Name, soPath)
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

// LoadAll loads every declared plugin through the given Loader.
func LoadAll(loader Loader, specs []parse.PluginSpec) ([]sdk.Plugin, error) {
	loaded := make([]sdk.Plugin, len(specs))
	for i, spec := range specs {
		p, err := loader.Load(spec)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin %s: %w", spec.Name, err)
		}
		loaded[i] = p
	}
	return loaded, nil
}
