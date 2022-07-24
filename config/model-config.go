package config

type Header struct {
	Kind string `yaml:"kind"`
}

type PipelineRaw struct {
	Producer     map[string]interface{}   `yaml:"producer"`
	Consumer     map[string]interface{}   `yaml:"consumer"`
	Transformers []map[string]interface{} `yaml:"transformers"`
}

type ConfigRaw struct {
	Pipelines map[string]PipelineRaw `yaml:"pipelines"`
}
