package core

import (
	"github.com/gastrodon/psyduck/configure"
	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-std/sdk"
)

func all(a []bool) bool {
	for _, t := range a {
		if !t {
			return false
		}
	}

	return true
}

func joinProducers(producers []sdk.Producer, _ []*configure.Resource) sdk.Producer {
	prodSize := len(producers)
	funcs := make([]sdk.Producefunc, prodSize)
	dones := make([]bool, prodSize)
	for i, producer := range producers {
		funcs[i] = producer()
	}

	return func() sdk.Producefunc {
		i := 0

		return func() ([]byte, bool, error) {
			// TODO do the not branching math when you are not lazy ( but I'm lazy rn )
			// I think it's i += (i+len(funcs)) % len(funcs) or something like it
			if all(dones) {
				return nil, true, nil
			}

			// TODO wary of this code...
			for i += (i + prodSize) % prodSize; !dones[i]; i += (i + prodSize) % prodSize {
			}

			// TODO different producers will be done at different times
			// they should be dropped from the pool, maybe [len(funcs)]bool in which to mark them done
			// and skip i over the the done ones?
			// TODO also we are returning whether or not this prodfunc is done
			// when really we should return done when all prodfuncs are done
			next, done, err := funcs[i]()
			if err != nil {
				return nil, false, err
			}

			dones[i] = done
			return next, all(dones), nil
		}
	}
}

func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	funcs := make([]sdk.Consumefunc, len(consumers))
	for i, consumer := range consumers {
		funcs[i] = consumer()
	}

	return func() sdk.Consumefunc {
		return func(b []byte) error {
			for _, f := range funcs {
				if err := f(b); err != nil {
					// TODO this behavior should be governed by the exit-on-error config
					return err
				}
			}

			return nil
		}
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
