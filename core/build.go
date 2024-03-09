package core

import (
	"github.com/gastrodon/psyduck/configure"
	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-std/sdk"
)

/*
Join a collection of producers into a single in the order received
*/
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

/*
Join a collection of consumers into a single that passes data to consumers in order
*/
func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	return func() (chan []byte, chan error, chan bool) {
		chanData := make([]chan []byte, len(consumers))
		chanErrors := make([]chan error, len(consumers))
		chanDones := make([]chan bool, len(consumers))

		for index, consumer := range consumers {
			chanConsumer, chanError, chanDone := consumer()
			chanData[index] = chanConsumer
			chanErrors[index] = chanError
			chanDones[index] = chanDone
		}

		joined := make(chan []byte)
		go func() {
			for data := range joined {
				for index := range chanData {
					chanData[index] <- data
				}
			}

			for index := range chanData {
				close(chanData[index])
			}
		}()

		errors := make(chan error)
		go func() {
			for err := range errors {
				for index := range chanErrors {
					chanErrors[index] <- err
				}
			}
		}()

		doneAll := make(chan bool)
		doneCollect := make(chan bool)
		go func() {
			doneLimit := len(consumers)
			for collected := range doneCollect {
				if !collected {
					panic("false sent through done channel")
				}

				doneLimit--
				if doneLimit <= 0 {
					if doneLimit < 0 {
						panic("doneLimit < 0! this should never happen")
					}

					doneAll <- true
					return
				}
			}
		}()

		for _, chanDone := range chanDones {
			go func(each chan (bool)) {
				done := <-each
				doneCollect <- done
			}(chanDone)
		}

		return joined, errors, doneAll
	}
}

/*
Join a collection of transformers into a single that applies them in order
*/
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

/*
descriptor is a parsed `pipeline {}` block of hcl
context is an hcl evaluation context, used to resolve values in descriptor
library ( TODO - deprecated ) has content loaded from plugins

Produces a runnable pipeline.

Each mover in the pipeline ( every producer / consumer / transformer ) is joined
and the resulting pipeline is returned.
*/
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
