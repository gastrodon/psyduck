package core

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"

	"github.com/gastrodon/psyduck/parse"
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

// throttle returns a wait func pacing calls to perMinute per minute.
// Non-positive perMinute means unrestricted.
func throttle(perMinute int) func() {
	if perMinute <= 0 {
		return func() {}
	}

	tick := time.NewTicker(time.Minute / time.Duration(perMinute))
	return func() { <-tick.C }
}

// applyMetaProducer wraps a producer with host-owned BlockMeta behavior:
// per-minute rate limiting and stop-after item cutoff.
func applyMetaProducer(produce sdk.Producer, meta sdk.BlockMeta) sdk.Producer {
	if meta.PerMinute <= 0 && meta.StopAfter <= 0 {
		return produce
	}

	return func(send chan<- []byte, errs chan<- error) {
		inner := make(chan []byte)
		go produce(inner, errs)

		wait, count := throttle(meta.PerMinute), 0
		for msg := range inner {
			wait()
			send <- msg
			count++
			if meta.StopAfter > 0 && count >= meta.StopAfter {
				break
			}
		}

		close(send)
	}
}

// applyMetaConsumer wraps a consumer with host-owned BlockMeta behavior:
// per-minute rate limiting and stop-after item cutoff.
func applyMetaConsumer(consume sdk.Consumer, meta sdk.BlockMeta) sdk.Consumer {
	if meta.PerMinute <= 0 && meta.StopAfter <= 0 {
		return consume
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		inner := make(chan []byte)
		go consume(inner, errs, done)

		wait, count := throttle(meta.PerMinute), 0
		for msg := range recv {
			wait()
			inner <- msg
			count++
			if meta.StopAfter > 0 && count >= meta.StopAfter {
				break
			}
		}

		close(inner)
	}
}

// bindChunk is how many bindings we ask a Bindings stream for at a time.
const bindChunk = 8

// drain exhausts a Bindings stream, binding each against its owning plugin
// and handing the configured instance to collect.
func drain(bindings parse.Resources, plugins map[string]sdk.Plugin, collect func(parse.Resource, sdk.Instance)) error {
	for {
		chunk, err := bindings(bindChunk)
		if err != nil {
			return err
		}
		if len(chunk) == 0 {
			return nil
		}

		for _, b := range chunk {
			plugin, ok := plugins[b.PluginID]
			if !ok {
				return fmt.Errorf("%s: %s: no plugin %q loaded", b.Block.Origin(), b.Ref, b.PluginID)
			}

			instance, err := plugin.Bind(b.Kind, b.Resource.Name, b.Block)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", b.Block.Origin(), b.Ref, err)
			}

			collect(b, instance)
		}
	}
}

/*
BuildPipeline turns a parsed pipeline description into a runnable Pipeline.

Each binding is resolved against its owning plugin, wrapped with host-owned
BlockMeta behavior, and joined with its siblings.
*/
func BuildPipeline(src parse.Pipeline, plugins map[string]sdk.Plugin) (*Pipeline, error) {
	logger := pipelineLogger()

	producers := make([]sdk.Producer, 0)
	if err := drain(src.Producers, plugins, func(b parse.Resource, instance sdk.Instance) {
		producers = append(producers, applyMetaProducer(instance.Produce, b.Meta))
	}); err != nil {
		return nil, err
	}
	if len(producers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no producers", src.Origin, src.Name)
	}

	consumers := make([]sdk.Consumer, 0)
	if err := drain(src.Consumers, plugins, func(b parse.Resource, instance sdk.Instance) {
		consumers = append(consumers, applyMetaConsumer(instance.Consume, b.Meta))
	}); err != nil {
		return nil, err
	}
	if len(consumers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no consumers", src.Origin, src.Name)
	}

	transformers := make([]sdk.Transformer, 0)
	if err := drain(src.Transformers, plugins, func(b parse.Resource, instance sdk.Instance) {
		transformers = append(transformers, instance.Transform)
	}); err != nil {
		return nil, err
	}

	return &Pipeline{
		Producer:    joinProducers(producers, logger),
		Consumer:    joinConsumers(consumers, logger),
		Transformer: stackTransform(transformers),
		logger:      logger,
		StopAfter:   src.StopAfter,
		ExitOnError: src.ExitOnError,
	}, nil
}
