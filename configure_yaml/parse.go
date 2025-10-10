// Package configure_yaml provides basic YAML parsing utilities for configuration data.
package configure_yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// fromString parses the full YAML text into a Config structure.
func fromString(yamlText string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FromContent parses the full YAML configuration and returns pipelines.
// For YAML, EvalContext is not applicable, so returns nil.
func FromContent(filename string, literal []byte) (*Config, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	cfg := new(Config)
	if err = yaml.Unmarshal([]byte(string(content)), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return cfg, nil
}

// FromDir reads all .yaml or .yml files in the directory and concatenates them.
// Similar to configure.FromDir but for YAML files.
func FromDir(directory string) ([]byte, error) {
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
