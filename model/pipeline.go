package model

type Descriptor struct {
	Kind   string
	Config map[string]interface{}
}

type PipelineDescriptor struct {
	Producer     Descriptor
	Consumer     Descriptor
	Transformers []Descriptor
}

type Pipeline struct {
	Producer           Producer
	Consumer           Consumer
	Transformers       []Transformer
	StackedTransformer Transformer
}
