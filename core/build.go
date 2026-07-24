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

// collectResources drains a resource stream fully into a slice without
// binding anything. Used for transformers, which need their per-resource
// Parallel count in hand (to bind each one n times) before any instance is
// created — unlike drain, which binds one instance per resource as it goes.
func collectResources(ctx context.Context, stream parse.ResourceFunc) ([]parse.Resource, error) {
	var out []parse.Resource
	for {
		chunk, err := stream(ctx, bindChunk)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			return out, nil
		}
		out = append(out, chunk...)
	}
}

// parallelTransformer builds one transform stage from n independently-bound
// instances of the same resource. Every instance reads from the stage's single
// input channel, so each incoming message is picked up by whichever copy is
// free — greedy load-balancing, not duplication — and their outputs merge into
// the stage's one output. Ordering across the stage is therefore not preserved,
// which is the cost of the parallelism the source opted into with parallel = n.
//
// The transformer contract has each instance close its own out channel on
// return, so the copies cannot share one: each gets a private sub channel that
// a forwarder drains into the merged out. The stage owns closing out, once all
// copies have finished and their forwarders have drained. errs is shared — the
// contract never closes it, and multiple senders are fine.
//
// With a single instance the stage is the plain transformer, no fan-out
// machinery — the overwhelmingly common non-parallel path stays untouched.
func parallelTransformer(instances []sdk.Instance, logger *logrus.Logger) sdk.Transformer {
	closeInstance := func(instance sdk.Instance) {
		if err := instance.Close(); err != nil {
			logger.WithError(err).Warn("failed to close transformer instance")
		}
	}

	if len(instances) == 1 {
		instance := instances[0]
		return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
			defer closeInstance(instance)
			instance.Transform(ctx, in, out, errs)
		}
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		var wg sync.WaitGroup
		for _, instance := range instances {
			sub := make(chan []byte)
			wg.Add(2)

			go func(instance sdk.Instance) {
				defer wg.Done()
				defer closeInstance(instance)
				instance.Transform(ctx, in, sub, errs)
			}(instance)

			go func() {
				defer wg.Done()
				for {
					select {
					case msg, ok := <-sub:
						if !ok {
							return
						}
						select {
						case out <- msg:
						case <-ctx.Done():
							return
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		wg.Wait()
	}
}

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

			instance, err := plugin.Bind(ctx, b.Kind, b.Resource.Name, b.Block)
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
	// plugins, the server-side handle itself). Close errors are logged via
	// logrus but not propagated: by the time a stage returns, its error
	// channel is no longer safe to send on (consumers and producers close
	// their own errs, and the transform forwarder may already be gone).
	// Close failures are rare and typically indicate a dying subprocess;
	// logging is sufficient.
	consumers := make([]sdk.Consumer, 0)
	if err := drain(ctx, src.Consumers, lookup, func(b parse.Resource, instance sdk.Instance) {
		consume := flow.Consumer(instance.Consume, b.Meta.PerMinute, 0)
		consumers = append(consumers, func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			defer func() {
				if err := instance.Close(); err != nil {
					logger.WithError(err).Warn("failed to close consumer instance")
				}
			}()
			consume(ctx, recv, errs, done)
		})
	}); err != nil {
		return nil, err
	}
	if len(consumers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no consumers", src.Origin, src.Name)
	}

	// Transformers are bound eagerly, one stage per declared resource. A
	// resource with Parallel > 1 is bound that many times and its instances
	// fanned out (see parallelTransformer), so the copies process incoming
	// messages greedily in parallel rather than being chained.
	transResources, err := collectResources(ctx, src.Transformers)
	if err != nil {
		return nil, err
	}
	transformers := make([]sdk.Transformer, 0, len(transResources))
	for _, b := range transResources {
		plugin, ok := lookup[b.PluginID]
		if !ok {
			return nil, fmt.Errorf("%s: %s: no plugin %q loaded", b.Block.Origin(), b.Ref, b.PluginID)
		}
		n := max(b.Parallel, 1)
		instances := make([]sdk.Instance, 0, n)
		for range n {
			instance, err := plugin.Bind(ctx, b.Kind, b.Resource.Name, b.Block)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", b.Block.Origin(), b.Ref, err)
			}
			instances = append(instances, instance)
		}
		transformers = append(transformers, parallelTransformer(instances, logger))
	}

	return &Pipeline{
		Producers:   producerSource(src.Producers, lookup, logger),
		Parallel:    src.ProduceParallel,
		Consumers:   consumers,
		Transformer: composeTransformers(transformers),
		logger:      logger,
		ExitOnError: src.ExitOnError,
	}, nil
}
