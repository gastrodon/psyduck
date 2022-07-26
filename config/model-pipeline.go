package config

type Descriptor struct {
	Kind   string
	Config map[string]interface{}
}

type PipelineDescriptor struct {
	Producers    []*Descriptor
	Consumers    []*Descriptor
	Transformers []*Descriptor
}

type PipelineDescriptors map[string]*PipelineDescriptor
