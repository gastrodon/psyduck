package parse

// PartYAML represents a pipeline part (producer, consumer, transformer) in YAML format.
type PartYAML struct {
	Kind    string                 `yaml:"kind"`
	Name    string                 `yaml:"name"`
	Options map[string]interface{} `yaml:",inline"`
}

// PipelineDesc represents a pipeline configuration in YAML format.
type PipelineDesc struct {
	Name         string     `yaml:"name"`
	ProduceFrom  *PartYAML  `yaml:"produce-from,omitempty"`
	Produce      []PartYAML `yaml:"produce,omitempty"`
	Consumers    []PartYAML `yaml:"consume"`
	Transformers []PartYAML `yaml:"transform"`
	StopAfter    int        `yaml:"stop-after,omitempty"`
	ExitOnError  bool       `yaml:"exit-on-error,omitempty"`
}
