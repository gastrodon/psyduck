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

func Parse(content string) (*Config, error) {
	cfg := new(Config)
	err := parse("yaml", content, cfg)
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

// ParseDir reads all .yaml or .yml files in the directory and concatenates them.
// Similar to configure.ParseDir but for YAML files.
func ParseDir(directory string) ([]byte, error) {
	var literal strings.Builder
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || (!strings.HasSuffix(info.Name(), ".yaml") && !strings.HasSuffix(info.Name(), ".yml")) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed reading %s: %w", path, err)
		}
		literal.Write(content)
		literal.WriteString("\n")
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []byte(literal.String()), nil
}
