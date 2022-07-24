package config

type Descriptor struct {
	Kind   string
	Config map[string]interface{}
}

type PipelineDescriptor struct {
	Producer     Descriptor
	Consumer     Descriptor
	Transformers []Descriptor
}

type PipelineDescriptors map[string]*PipelineDescriptor
