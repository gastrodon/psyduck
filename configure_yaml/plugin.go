package configure_yaml

import (
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
)

// PipelineParts represents partial pipeline parts parsed from YAML.
type PipelineParts struct {
	Produce   []PartYAML
	Consume   PartYAML
	Transform PartYAML
}

// FetchPlugins fetches plugins from YAML configuration.
func FetchPlugins(initPath, filename string, literal []byte, _ *hcl.EvalContext) (map[string]string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filename, err)
	}
	plugins, err := fromString(string(content))
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
func LoadPlugins(initPath, filename string, literal []byte, evalCtx *hcl.EvalContext) ([]*sdk.Plugin, error) {
	panic("TODO: YAML plugin loading not implemented")
}
