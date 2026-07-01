package datasource

import (
	"fmt"
	"plugin"

	"github.com/psyduck-etl/sdk"
)

func LoadPluginBinary(name, soPath string) (*sdk.Plugin, error) {
	p, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin %s at %s: %w", name, soPath, err)
	}

	sym, err := p.Lookup("Plugin")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup Plugin symbol for %s: %w", name, err)
	}

	makePlugin, ok := sym.(func() *sdk.Plugin)
	if !ok {
		return nil, fmt.Errorf("Plugin symbol for %s is not func() *sdk.Plugin: %+v", name, sym)
	}

	return makePlugin(), nil
}

func Plugin(binPaths map[string]string) (Datasource[*sdk.Plugin], error) {
	plugins := make(map[string]*sdk.Plugin, len(binPaths))
	for name, path := range binPaths {
		p, err := LoadPluginBinary(name, path)
		if err != nil {
			return nil, err
		}
		plugins[name] = p
	}
	return &mapDatasource[*sdk.Plugin]{data: plugins}, nil
}
