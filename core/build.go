package core

import (
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/sdk"
)

func joinProducers(producers []sdk.Producer) sdk.Producer {
	return func(signal chan string) (chan []byte, error) {
		joined := make(chan []byte, len(producers))

		for _, producer := range producers {
			chanProducer, err := producer(signal)
			if err != nil {
				return nil, err
			}

			go func() {
				for data := range chanProducer {
					joined <- data
				}
			}()
		}

		return joined, nil
	}
}

func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	return func(signal chan string) (chan []byte, error) {
		chanConsumers := make([]chan []byte, len(consumers))
		for index, consumer := range consumers {
			chanConsumer, err := consumer(signal)
			if err != nil {
				return nil, err
			}

			chanConsumers[index] = chanConsumer
		}

		joined := make(chan []byte, len(consumers))
		go func() {
			for data := range joined {
				for index := range chanConsumers {
					chanConsumers[index] <- data
				}
			}
		}()

		return joined, nil
	}
}

func stackTransform(transformers []sdk.Transformer) sdk.Transformer {
	if len(transformers) == 0 {
		return func(data []byte) ([]byte, error) { return data, nil }
	}

	if len(transformers) == 1 {
		return transformers[0]
	}

	tail := len(transformers) - 1

	return func(data []byte) ([]byte, error) {
		transformed, err := stackTransform(transformers[:tail])(data)
		if err != nil {
			return nil, err
		}

		return transformers[tail](transformed)
	}
}

func BuildPipeline(descriptor *config.PipelineDescriptor, library *Library) (*Pipeline, error) {
	producers := make([]sdk.Producer, len(descriptor.Producers))
	for index, produceDescriptor := range descriptor.Producers {
		producer, err := library.ProvideProducer(produceDescriptor.Kind, produceDescriptor.Config)
		if err != nil {
			return nil, err
		}

		producers[index] = producer
	}

	consumers := make([]sdk.Consumer, len(descriptor.Consumers))
	for index, consumeDescriptor := range descriptor.Consumers {
		consumer, err := library.ProvideConsumer(consumeDescriptor.Kind, consumeDescriptor.Config)
		if err != nil {
			return nil, err
		}

		consumers[index] = consumer
	}

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformer, err := library.ProvideTransformer(transformDescriptor.Kind, transformDescriptor.Config)
		if err != nil {
			return nil, err
		}

		transformers[index] = transformer
	}

	return &Pipeline{
		Producer:           joinProducers(producers),
		Consumer:           joinConsumers(consumers),
		StackedTransformer: stackTransform(transformers),
	}, nil
}
