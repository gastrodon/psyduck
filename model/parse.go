package model

type DescriptorHeader struct {
	Kind string `yaml:"kind"`
}

type ETLPipelineRaw struct {
	Producer     map[string]interface{}   `yaml:"producer"`
	Consumer     map[string]interface{}   `yaml:"consumer"`
	Transformers []map[string]interface{} `yaml:"transformers"`
}

type ETLConfigRaw struct {
	Pipelines map[string]ETLPipelineRaw `yaml:"pipelines"`
}

type ETLConfig struct {
	Pipelines map[string]PipelineDescriptor
}
