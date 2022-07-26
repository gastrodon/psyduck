package core

import (
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/sdk"
)

func joinProducers(producers []sdk.Producer) sdk.Producer {
	return func(signal chan string) chan interface{} {
		joined := make(chan interface{}, len(producers))
		for _, producer := range producers {
			go func() {
				for data := range producer(signal) {
					joined <- data
				}
			}()
		}

		return joined
	}
}

func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	return func(signal chan string) chan interface{} {
		chanConsumers := make([]chan interface{}, len(consumers))
		for index, consumer := range consumers {
			chanConsumers[index] = consumer(signal)
		}

		joined := make(chan interface{}, len(consumers))
		go func() {
			for data := range joined {
				for index := range chanConsumers {
					chanConsumers[index] <- data
				}
			}
		}()

		return joined
	}
}

func stackTransform(transformers []sdk.Transformer) sdk.Transformer {
	if len(transformers) == 0 {
		return func(data interface{}) interface{} { return data }
	}

	if len(transformers) == 1 {
		return transformers[0]
	}

	tail := len(transformers) - 1

	return func(data interface{}) interface{} {
		return transformers[tail](stackTransform(transformers[:tail])(data))
	}
}

func BuildPipeline(descriptor *config.PipelineDescriptor, library *Library) *Pipeline {
	producers := make([]sdk.Producer, len(descriptor.Producers))
	for index, produceDescriptor := range descriptor.Producers {
		producers[index] = library.ProvideProducer(produceDescriptor.Kind, produceDescriptor.Config)
	}

	consumers := make([]sdk.Consumer, len(descriptor.Consumers))
	for index, consumeDescriptor := range descriptor.Consumers {
		consumers[index] = library.ProvideConsumer(consumeDescriptor.Kind, consumeDescriptor.Config)
	}

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformers[index] = library.ProvideTransformer(transformDescriptor.Kind, transformDescriptor.Config)
	}

	return &Pipeline{
		Producer:           joinProducers(producers),
		Consumer:           joinConsumers(consumers),
		StackedTransformer: stackTransform(transformers),
	}
}
