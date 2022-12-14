package core

import (
	"github.com/gastrodon/psyduck/configure"
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
)

func joinProducers(producers []sdk.Producer) sdk.Producer {
	return func() (chan []byte, chan error) {
		joined := make(chan []byte, len(producers))
		errors := make(chan error)
		closed := 0

		for index, producer := range producers {
			chanData, chanError := producer()

			go func(index int) {
				for {
					select {
					case dataNext := <-chanData:
						if dataNext == nil {
							closed++
							if closed == len(producers) {
								close(joined)
								close(errors)
							}

							return
						}

						joined <- dataNext
					case errNext := <-chanError:
						if errNext == nil {
							continue
						}

						errors <- errNext
					}
				}
			}(index)
		}

		return joined, errors
	}
}

func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	return func() (chan []byte, chan error) {
		chanData := make([]chan []byte, len(consumers))
		chanErrors := make([]chan error, len(consumers))

		for index, consumer := range consumers {
			chanConsumer, chanError := consumer()
			chanData[index] = chanConsumer
			chanErrors[index] = chanError
		}

		joined := make(chan []byte, len(consumers))
		go func() {
			for data := range joined {
				for index := range chanData {
					chanData[index] <- data
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

func BuildPipeline(descriptor *configure.Pipeline, context *hcl.EvalContext, library *Library) (*Pipeline, error) {
	producers := make([]sdk.Producer, len(descriptor.Producers))
	for index, produceDescriptor := range descriptor.Producers {
		producer, err := library.ProvideProducer(produceDescriptor.Kind, context, produceDescriptor.Options)
		if err != nil {
			return nil, err
		}

		producers[index] = producer
	}

	consumers := make([]sdk.Consumer, len(descriptor.Consumers))
	for index, consumeDescriptor := range descriptor.Consumers {
		consumer, err := library.ProvideConsumer(consumeDescriptor.Kind, context, consumeDescriptor.Options)
		if err != nil {
			return nil, err
		}

		consumers[index] = consumer
	}

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformer, err := library.ProvideTransformer(transformDescriptor.Kind, context, transformDescriptor.Options)
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
