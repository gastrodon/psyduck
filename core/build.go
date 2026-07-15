package core

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/stdlib/flow"
)

// Pipeline is a runnable pipeline. Producers are a run-time source rather
// than a fixed slice: calling it starts a feeder that binds producers lazily
// and streams them into RunPipeline's worker pool (see ProducerSource), which
// is what lets a produce-from seed keep declaring new producers for the life
// of the run. Parallel caps how many run at once. Consumers stay a slice —
// RunPipeline owns fan-out — and transformers are pre-stacked into one
// function.
type Pipeline struct {
	Producers   ProducerSource
	Parallel    int
	Consumers   []sdk.Consumer
	Transformer sdk.Transformer
	logger      *logrus.Logger
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

/*
composeTransformers joins a collection of transformers into a single one
that chains them: the first transformer's out feeds the second's in, and so
on, with every stage sharing one errs channel. Composition happens once at
build time, so RunPipeline spawns the resulting chain once per run instead
of recomposing per message.

Zero transformers composes to nil, which RunPipeline treats as "no
transform stage" and bypasses entirely.
*/
func composeTransformers(transformers []sdk.Transformer) sdk.Transformer {
	switch len(transformers) {
	case 0:
		return nil
	case 1:
		return transformers[0]
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		var wg sync.WaitGroup
		stage := in
		for _, t := range transformers[:len(transformers)-1] {
			next := make(chan []byte)
			wg.Add(1)
			go func(t sdk.Transformer, in <-chan []byte, out chan<- []byte) {
				defer wg.Done()
				t(ctx, in, out, errs)
			}(t, stage, next)
			stage = next
		}
		transformers[len(transformers)-1](ctx, stage, out, errs)
		wg.Wait()
	}
}

// bindChunk is how many bindings we ask a Bindings stream for at a time.
const bindChunk = 8

// drain exhausts a Bindings stream, binding each against its owning plugin
// and handing the configured instance to collect. Draining can do real work
// (produce-from runs its seed) so it is bounded by ctx.
func drain(ctx context.Context, bindings parse.ResourceFunc, plugins map[string]sdk.Plugin, collect func(parse.Resource, sdk.Instance)) error {
	for {
		chunk, err := bindings(ctx, bindChunk)
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

Consumers and transformers are drained and bound eagerly here. Producers
are not: literal and produce-from pipelines alike are wrapped into a single
ProducerSource (see producerSource) that binds lazily at run time. So a dead
seed, an unknown producer plugin, or a broken producer config now surfaces at
run time through the pipeline's error reporting rather than at build — subject
to exit-on-error like any other producer error.
*/
func BuildPipeline(ctx context.Context, src parse.Pipeline, plugins []sdk.Plugin) (*Pipeline, error) {
	logger := pipelineLogger()

	lookup := make(map[string]sdk.Plugin, len(plugins))
	for _, p := range plugins {
		lookup[p.Name()] = p
	}

	// Each bound instance is closed when its stage function returns — Close
	// is how a plugin releases whatever Bind acquired (for subprocess
	// plugins, the server-side handle itself). Close errors are dropped,
	// matching the produce-from seed: by the time a stage returns, its error
	// channel is no longer safe to send on (consumers and producers close
	// their own errs, and the transform forwarder may already be gone).
	consumers := make([]sdk.Consumer, 0)
	if err := drain(ctx, src.Consumers, lookup, func(b parse.Resource, instance sdk.Instance) {
		consume := flow.Consumer(instance.Consume, b.Meta.PerMinute, 0)
		consumers = append(consumers, func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			defer instance.Close()
			consume(ctx, recv, errs, done)
		})
	}); err != nil {
		return nil, err
	}
	if len(consumers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no consumers", src.Origin, src.Name)
	}

	transformers := make([]sdk.Transformer, 0)
	if err := drain(ctx, src.Transformers, lookup, func(b parse.Resource, instance sdk.Instance) {
		transformers = append(transformers, func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
			defer instance.Close()
			instance.Transform(ctx, in, out, errs)
		})
	}); err != nil {
		return nil, err
	}

	return &Pipeline{
		Producers:   producerSource(src.Producers, lookup),
		Parallel:    src.ProduceParallel,
		Consumers:   consumers,
		Transformer: composeTransformers(transformers),
		logger:      logger,
		ExitOnError: src.ExitOnError,
	}, nil
}
