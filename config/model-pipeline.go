package config

type PipelineRaw struct {
	Name           string   `hcl:"name,label" cty:"name"`
	ProducerRef    []string `hcl:"producers" cty:"producers"`
	ConsumerRef    []string `hcl:"consumers" cty:"consumers"`
	TransformerRef []string `hcl:"transformers" cty:"transformers"`
}

type PipelinesRaw struct {
	Pipelines []*PipelineRaw `hcl:"pipeline,block" cty:"pipeline"`
}

type Pipeline struct {
	Name         string
	Producers    []*Resource
	Consumers    []*Resource
	Transformers []*Resource
}

type Pipelines struct {
	Pipelines map[string]*Pipeline
}
