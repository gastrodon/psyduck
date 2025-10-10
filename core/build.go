package core

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gastrodon/psyduck/parse"
	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"
)

type Pipeline struct {
	Producer    sdk.Producer
	Consumer    sdk.Consumer
	Transformer sdk.Transformer
	logger      *logrus.Logger
	StopAfter   int
	ExitOnError bool
}

func pipelineLogger() *logrus.Logger {
	l := logrus.New()
	l.ReportCaller = true

	switch os.Getenv("PSYDUCK_LOG_LEVEL") {
	case "trace":
		l.SetLevel(logrus.TraceLevel)
	case "debug":
		l.SetLevel(logrus.DebugLevel)
	case "warn":
		l.SetLevel(logrus.WarnLevel)
	case "error":
		l.SetLevel(logrus.ErrorLevel)
	case "fatal":
		l.SetLevel(logrus.FatalLevel)
	case "panic":
		l.SetLevel(logrus.PanicLevel)
	default:
		l.SetLevel(logrus.InfoLevel)
	}

	return l
}

func mchan[T any](c int) []chan T {
	g := make([]chan T, c)
	for i := range g {
		g[i] = make(chan T)
	}
	return g
}

func join[T any](group []chan T, ent *logrus.Entry) chan T {
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

		ent.Trace("closing all of the groups")
		close(joined)
	}(len(group))

	for i := range group {
		go func(ishadow int, closer chan<- struct{}) { // goroutine forwarder for every c in group
			for msg := range group[ishadow] {
				joined <- msg
			}

			ent.Tracef("forwarder %d exhausted", ishadow)
			closer <- struct{}{}
		}(i, closer)
	}

	return joined
}

/*
Join a collection of producers into a single in the order received
*/
func joinProducers(producers []sdk.Producer, logger *logrus.Logger) sdk.Producer {
	if len(producers) == 1 {
		return producers[0]
	}

	gData := mchan[[]byte](len(producers))
	gErrs := mchan[error](len(producers))

	tData := join(gData, logger.WithField("joined", "data"))
	tErrs := join(gErrs, logger.WithField("joined", "errs"))
	return func(dataOut chan<- []byte, errs chan<- error) {
		for i := 0; i < len(producers); i++ {
			go producers[i](gData[i], gErrs[i])
		}

	out:
		for {
			select {
			case msg := <-tData:
				if msg == nil {
					logger.Trace("tData closed, breaking out")
					break out
				}

				dataOut <- msg
			case err := <-tErrs:
				if err != nil {
					logger.Error(err)
					errs <- err
				}
			}
		}

		logger.Trace("closing dataOut")
		close(dataOut)
	}
}

/*
Join a collection of consumers into a single that passes data to consumers in order
*/
func joinConsumers(consumers []sdk.Consumer, logger *logrus.Logger) sdk.Consumer {
	if len(consumers) == 1 {
		return consumers[0]
	}

	gErrs := mchan[error](len(consumers))
	gDone := mchan[struct{}](len(consumers))
	split := mchan[[]byte](len(consumers))

	tErrs := join(gErrs, logger.WithField("joined", "errs"))
	tDone := join(gDone, logger.WithField("joined", "done"))
	return func(dataRecv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		for i := range split {
			go consumers[i](split[i], gErrs[i], gDone[i])
		}

		handle := make(chan error)

		go func() {
			for msg := range dataRecv {
				for i := range split {
					split[i] <- msg
					logger.Tracef("fwd to split[%d]\n", i)
				}
			}

			close(handle)
		}()

		go func() {
			for err := range tErrs {
				if err != nil {
					handle <- err
				}
			}
		}()

		for err := range handle {
			if err != nil {
				errs <- err
			}
		}

		for i := range split {
			logger.Tracef("closing split[%d]\n", i)
			close(split[i])
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

		logger.Trace("closing provided done")
		close(done)
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
		if err != nil || transformed == nil {
			return nil, err
		}

		return transformers[tail](transformed)
	}
}

func collectProducer(descriptor *parse.PipelineDesc, library Library, logger *logrus.Logger) (sdk.Producer, error) {
	if descriptor.ProduceFrom != nil {
		logger.Trace("getting remote producer")
		p, err := library.Producer(descriptor.ProduceFrom.Kind, descriptor.ProduceFrom.Options)
		if err != nil {
			return nil, fmt.Errorf("failed providing remote producer: %s", err)
		}

		// this is already scuffed
		t := time.NewTimer(10 * time.Second)
		defer t.Stop()
		send, errs := make(chan []byte), make(chan error)
		go p(send, errs)
		select {
		case <-t.C:
			return nil, fmt.Errorf("timeout getting anything from the meta-producer") // stupid name? hardcoded timeout? I will fix it later TODO
		case err := <-errs:
			return nil, fmt.Errorf("error getting from meta-producer: %s", err)
		case msg := <-send:
			parts, err := parse.ParseString("yaml", string(msg))
			if err != nil {
				return nil, fmt.Errorf("failed to configure remote: %s", err)
			}

			var producers []parse.PartYAML
			for _, p := range parts.Pipelines {
				producers = append(producers, p.Produce...)
			}

			return collectProducer(&parse.PipelineDesc{
				Name:         descriptor.Name,
				ProduceFrom:  nil,
				Produce:      producers,
				Consumers:    descriptor.Consumers,
				Transformers: descriptor.Transformers,
				StopAfter:    descriptor.StopAfter,
			}, library, logger)
		}
	}

	logger.Trace("config literal producer")
	switch len(descriptor.Produce) {
	case 0:
		return nil, fmt.Errorf("1 or more producer is required")
	case 1:
		logger.Trace("only one producer")
		return library.Producer(descriptor.Produce[0].Kind, descriptor.Produce[0].Options)
	default:
		producers := make([]sdk.Producer, len(descriptor.Produce))
		for index, produceDescriptor := range descriptor.Produce {
			producer, err := library.Producer(produceDescriptor.Kind, produceDescriptor.Options)
			if err != nil {
				return nil, err
			}

			producers[index] = producer
		}

		return joinProducers(producers, logger), nil
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
func BuildPipeline(descriptor *parse.PipelineDesc, library Library) (*Pipeline, error) {
	logger := pipelineLogger()
	producer, err := collectProducer(descriptor, library, logger)
	if err != nil {
		return nil, err
	}

	consumers := make([]sdk.Consumer, len(descriptor.Consumers))
	for index, consumeDescriptor := range descriptor.Consumers {
		consumer, err := library.Consumer(consumeDescriptor.Kind, consumeDescriptor.Options)
		if err != nil {
			return nil, err
		}

		consumers[index] = consumer
	}

	transformers := make([]sdk.Transformer, len(descriptor.Transformers))
	for index, transformDescriptor := range descriptor.Transformers {
		transformer, err := library.Transformer(transformDescriptor.Kind, transformDescriptor.Options)
		if err != nil {
			return nil, err
		}

		transformers[index] = transformer
	}

	return &Pipeline{
		Producer:    producer,
		Consumer:    joinConsumers(consumers, logger),
		Transformer: stackTransform(transformers),
		logger:      logger,
		StopAfter:   descriptor.StopAfter,
		ExitOnError: descriptor.ExitOnError,
	}, nil
}
