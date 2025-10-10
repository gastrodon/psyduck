// Package configure_yaml provides basic YAML parsing utilities for configuration data.
package configure_yaml

import (
	"io"

	"gopkg.in/yaml.v3"
)

// Parse reads YAML from the provided reader and unmarshals it into a generic map.
// TODO: Add more precise type information for configuration values.
func Parse(r io.Reader) (map[string]interface{}, error) {
	var data map[string]interface{}
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// ParseConfig parses the full YAML text into a Config structure.
func ParseConfig(yamlText string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParsePipelines parses the YAML text and returns the slice of PipelineYAML entries.
func ParsePipelines(yamlText string) ([]PipelineYAML, error) {
	cfg, err := ParseConfig(yamlText)
	if err != nil {
		return nil, err
	}
	return cfg.Pipelines, nil
}

// LoadPipelinesFromYAML is a convenience function that parses the YAML text
// and returns a slice of PipelineYAML definitions.
func LoadPipelinesFromYAML(yamlText string) ([]PipelineYAML, error) {
	return ParsePipelines(yamlText)
}
