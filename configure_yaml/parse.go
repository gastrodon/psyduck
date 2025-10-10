package configure_yaml

import (
	"gopkg.in/yaml.v3"
)

// Config represents the top-level configuration.
// It supports a sequence of pipelines.
type Config struct {
	Pipelines []PipelineYAML `yaml:"pipelines"`
	Plugins   []PluginYAML   `yaml:"plugins,omitempty"`
}

type Parseable interface {
	Name() string
	Parse() (*Config, error)
}

func parse(kind string, content string, cfg *Config) error {
	switch kind {
	case "":
		panic("no parser specified")
	case "yaml", "yml":
		return yaml.Unmarshal([]byte(content), cfg)
	default:
		panic("no parser for " + kind)
	}
}
