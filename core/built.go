package core

import (
	"github.com/gastrodon/psyduck/model"
)

func stackTransform(transformers []model.Transformer) model.Transformer {
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

func BuildPipeline(descriptor model.PipelineDescriptor, library model.Library) model.Pipeline {
	producer := library.ProvideProducer(descriptor.Producer.Kind, descriptor.Producer.Config)
	consumer := library.ProvideConsumer(descriptor.Consumer.Kind, descriptor.Consumer.Config)

	transformers := make([]model.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformers[index] = library.ProvideTransformer(transformDescriptor.Kind, transformDescriptor.Config)
	}

	return model.Pipeline{
		Producer:           producer,
		Consumer:           consumer,
		Transformers:       transformers,
		StackedTransformer: stackTransform(transformers),
	}
}
