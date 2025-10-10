package configure_yaml

import (
	"github.com/psyduck-etl/sdk"
)

// FetchPlugins fetches plugins from YAML configuration.
func FetchPlugins(initPath, filename string, literal []byte) (map[string]string, error) {
	plugins, err := Parse(newFileSRC(filename))
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, p := range plugins.Plugins {
		result[p.Name] = p.Source
	}
	return result, nil
}

// LoadPlugins loads plugins from YAML configuration.
// TODO: Implement YAML plugin loading
func LoadPlugins(initPath, filename string, literal []byte) ([]*sdk.Plugin, error) {
	panic("TODO: YAML plugin loading not implemented")
}
