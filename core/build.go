package core

import (
	"github.com/gastrodon/psyduck/config"
	"github.com/gastrodon/psyduck/sdk"
)

func makeAllDone(limit int, done func()) func() {
	count := 0

	return func() {
		count++
		if count == limit {
			done()
		}
	}
}

func joinProducers(producers []sdk.Producer) sdk.Producer {
	return func(signal chan string, done func()) (chan []byte, chan error) {
		allDone := makeAllDone(len(producers), done)
		joined := make(chan []byte, len(producers))
		errors := make(chan error)

		for _, producer := range producers {
			chanProducer, chanError := producer(signal, allDone)

			go func() {
				for {
					select {
					case dataNext := <-chanProducer:
						joined <- dataNext
					case errNext := <-chanError:
						errors <- errNext
					}
				}
			}()
		}

		return joined, nil
	}
}

func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	return func(signal chan string, done func()) (chan []byte, chan error) {
		allDone := makeAllDone(len(consumers), done)
		chanConsumers := make([]chan []byte, len(consumers))
		chanErrors := make([]chan error, len(consumers))

		for index, consumer := range consumers {
			chanConsumer, chanError := consumer(signal, allDone)
			chanConsumers[index] = chanConsumer
			chanErrors[index] = chanError
		}

		joined := make(chan []byte, len(consumers))
		go func() {
			for data := range joined {
				for index := range chanConsumers {
					chanConsumers[index] <- data
				}
			}
		}()

		errors := make(chan error, len(consumers))
		go func() {
			for err := range errors {
				for index := range chanErrors {
					chanErrors[index] <- err
				}
			}
		}()

		return joined, errors
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

func BuildPipeline(descriptor *config.Pipeline, library *Library) (*Pipeline, error) {
	producers := make([]sdk.Producer, len(descriptor.Producers))
	for index, produceDescriptor := range descriptor.Producers {
		producer, err := library.ProvideProducer(produceDescriptor.Kind, produceDescriptor.Remain)
		if err != nil {
			return nil, err
		}

		producers[index] = producer
	}

	consumers := make([]sdk.Consumer, len(descriptor.Consumers))
	for index, consumeDescriptor := range descriptor.Consumers {
		consumer, err := library.ProvideConsumer(consumeDescriptor.Kind, consumeDescriptor.Remain)
		if err != nil {
			return nil, err
		}

		consumers[index] = consumer
	}

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformer, err := library.ProvideTransformer(transformDescriptor.Kind, transformDescriptor.Remain)
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
