package core

import (
	"fmt"
	"sync"

	"github.com/gastrodon/psyduck/configure"
	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
)

type Pipeline struct {
	Producer    sdk.Producer
	Consumer    sdk.Consumer
	Transformer sdk.Transformer
}

func mchan[T any](c int) []chan T {
	g := make([]chan T, c)
	for i := range g {
		g[i] = make(chan T)
	}
	return g
}

func join[T any](group []chan T) chan T {
	joined := make(chan T)
	closer := make(chan struct{})

	go func(size int) {
		closed, cLock := 0, new(sync.Mutex)
		for closed < size {
			<-closer
			cLock.Lock()
			closed++
			cLock.Unlock()
		}

		fmt.Println("tee: all groups closed")
		close(joined)
	}(len(group))

	for i := range group {
		go func(ishadow int, closer chan<- struct{}) { // goroutine forwarder for every c in group
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("RECOVER tee: recovered from %d: %s\n", ishadow, err)
					panic(err)
				}
			}()

			for msg := range group[ishadow] {
				joined <- msg
			}

			closer <- struct{}{}
		}(i, closer)
	}

	return joined
}

/*
Join a collection of producers into a single in the order received
*/
func joinProducers(producers []sdk.Producer) sdk.Producer {
	if len(producers) == 1 {
		return producers[0]
	}

	gData := mchan[[]byte](len(producers))
	gErrs := mchan[error](len(producers))

	tData := join(gData)
	tErrs := join(gErrs)
	return func(dataOut chan<- []byte, errs chan<- error) {
		for i := 0; i < len(producers); i++ {
			go producers[i](gData[i], gErrs[i])
		}

	out:
		for {
			select {
			case msg := <-tData:
				if msg == nil {
					break out
				}

				dataOut <- msg
			case err := <-tErrs:
				errs <- err
			}
		}

		fmt.Println("joinProducers: closing dataOut")
		close(dataOut)
		fmt.Println("joinProducers: closing dataOut OK")
	}
}

/*
Join a collection of consumers into a single that passes data to consumers in order
*/
func joinConsumers(consumers []sdk.Consumer) sdk.Consumer {
	if len(consumers) == 1 {
		return consumers[0]
	}

	gErrs := mchan[error](len(consumers))
	gDone := mchan[struct{}](len(consumers))
	split := mchan[[]byte](len(consumers))

	tErrs := join(gErrs)
	tDone := join(gDone)
	return func(dataRecv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		for i := range split {
			go consumers[i](split[i], gErrs[i], gDone[i])
		}

	out:
		for {
			select {
			case msg := <-dataRecv:
				if msg == nil {
					break out
				}

				for i := range split {
					fmt.Printf("joinConsumers: fwd to split[%d]\n", i)
					split[i] <- msg
					fmt.Printf("joinConsumers: fwd to split[%d] OK\n", i)
				}

			case err := <-tErrs:
				errs <- err
			}
		}

		for i := range split {
			fmt.Printf("joinConsumers: close split[%d]\n", i)
			close(split[i])
			fmt.Printf("joinConsumers: close split[%d] OK\n", i)
		}

		closed, cLock := 0, new(sync.Mutex)
		for range tDone {
			cLock.Lock()
			closed++
			if closed == len(consumers) {
				break
			}

			cLock.Unlock()
		}

		fmt.Println("joinConsumers: close provided done")
		close(done)
		fmt.Println("joinConsumers: close provided done OK")
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
		Producer:    joinProducers(producers),
		Consumer:    joinConsumers(consumers),
		Transformer: stackTransform(transformers),
	}, nil
}
