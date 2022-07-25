package core

import (
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/sdk"
)

func stackTransform(transformers []sdk.Transformer) sdk.Transformer {
	if len(transformers) == 0 {
		return func(data interface{}) interface{} { return data }
	}

	if len(transformers) == 1 {
		return transformers[0]
	}

	return func(data interface{}) interface{} {
		return transformers[0](stackTransform(transformers[1:])(data))
	}
}

func BuildPipeline(descriptor *config.PipelineDescriptor, library *Library) *Pipeline {
	producer := library.ProvideProducer(descriptor.Producer.Kind, descriptor.Producer.Config)
	consumer := library.ProvideConsumer(descriptor.Consumer.Kind, descriptor.Consumer.Config)

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformers[index] = library.ProvideTransformer(transformDescriptor.Kind, transformDescriptor.Config)
	}

	return &Pipeline{
		Producer:           producer,
		Consumer:           consumer,
		Transformers:       transformers,
		StackedTransformer: stackTransform(transformers),
	}
}
