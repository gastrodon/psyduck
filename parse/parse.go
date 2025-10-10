package configure

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level configuration.
// It supports a sequence of pipelines.
type Config struct {
	Pipelines []PipelineDesc `yaml:"pipelines"`
	Plugins   []PluginDesc   `yaml:"plugins,omitempty"`
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

func ParseString(kind string, content string) (*Config, error) {
	cfg := new(Config)
	err := parse(kind, content, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func ParseFile(fp string) (*Config, error) {
	return newParseFile(fp).Parse()
}

// ParseDir reads all .yaml or .yml files in the directory and parses them into a Config.
// It merges the configurations from all files.
func ParseDir(directory string) (*Config, error) {
	cfg := &Config{}
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		parsedCfg, err := newParseFile(path).Parse()
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		cfg.Pipelines = append(cfg.Pipelines, parsedCfg.Pipelines...)
		cfg.Plugins = append(cfg.Plugins, parsedCfg.Plugins...)
		return nil
	})

	return cfg, err
}
