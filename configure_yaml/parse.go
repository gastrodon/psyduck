package configure_yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func parse(kind string, content string, cfg *Config) error {
	switch kind {
	case "yaml", "yml":
		return yaml.Unmarshal([]byte(content), cfg)
	default:
		panic("no parser for " + kind)
	}
}

func Parse(src ParseSRC) (*Config, error) {
	cont, err := src.Content()
	if err != nil {
		return nil, err
	}

	cfg := new(Config)
	err = parse(src.Format(), cont, cfg)
	return cfg, err
}

// ParseFile parses the full YAML configuration and returns pipelines.
// For YAML, EvalContext is not applicable, so returns nil.
func ParseFile(filename string) (*Config, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	cfg := new(Config)
	err = parse("yaml", string(content), cfg)
	return cfg, err
}

// ParseDir reads all .yaml or .yml files in the directory and parses them into a Config.
// It merges the configurations from all files.
func ParseDir(directory string) (*Config, error) {
	cfg := &Config{}
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (!strings.HasSuffix(info.Name(), ".yaml") && !strings.HasSuffix(info.Name(), ".yml")) {
			return nil
		}
		parsedCfg, err := Parse(newFileSRC(path))
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		cfg.Pipelines = append(cfg.Pipelines, parsedCfg.Pipelines...)
		cfg.Plugins = append(cfg.Plugins, parsedCfg.Plugins...)
		return nil
	})

	return cfg, err
}
