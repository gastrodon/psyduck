package config

type Header struct {
	Kind string `yaml:"kind"`
}

type PipelineRaw struct {
	Producers    []map[string]interface{} `yaml:"producers"`
	Consumers    []map[string]interface{} `yaml:"consumers"`
	Transformers []map[string]interface{} `yaml:"transformers"`
}

type ConfigRaw struct {
	Pipelines map[string]PipelineRaw `yaml:"pipelines"`
}
