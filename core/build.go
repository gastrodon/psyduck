package core

import (
	"context"
	"fmt"
	"os"

	"github.com/psyduck-etl/sdk"
	"github.com/sirupsen/logrus"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/stdlib/flow"
)

// Pipeline is a runnable pipeline: producers and consumers stay separate
// slices — RunPipeline owns merging and fan-out — and transformers are
// pre-stacked into one function.
type Pipeline struct {
	Producers   []sdk.Producer
	Consumers   []sdk.Consumer
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

/*
Join a collection of transformers into a single that applies them in order
*/
func stackTransform(transformers []sdk.Transformer) sdk.Transformer {
	if len(transformers) == 0 {
		return func(data []byte) ([]byte, bool, error) { return data, true, nil }
	}

	if len(transformers) == 1 {
		return transformers[0]
	}

	tail := len(transformers) - 1

	return func(data []byte) ([]byte, bool, error) {
		transformed, keep, err := stackTransform(transformers[:tail])(data)
		if err != nil || !keep {
			return nil, false, err
		}

		return transformers[tail](transformed)
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
*/
func BuildPipeline(ctx context.Context, src parse.Pipeline, plugins []sdk.Plugin) (*Pipeline, error) {
	logger := pipelineLogger()

	lookup := make(map[string]sdk.Plugin, len(plugins))
	for _, p := range plugins {
		lookup[p.Name()] = p
	}

	producers := make([]sdk.Producer, 0)
	if err := drain(ctx, src.Producers, lookup, func(b parse.Resource, instance sdk.Instance) {
		producers = append(producers, flow.Producer(instance.Produce, b.Meta.PerMinute, 0, b.Meta.StopAfter))
	}); err != nil {
		return nil, err
	}
	if len(producers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no producers", src.Origin, src.Name)
	}

	consumers := make([]sdk.Consumer, 0)
	if err := drain(ctx, src.Consumers, lookup, func(b parse.Resource, instance sdk.Instance) {
		consumers = append(consumers, flow.Consumer(instance.Consume, b.Meta.PerMinute, 0, b.Meta.StopAfter))
	}); err != nil {
		return nil, err
	}
	if len(consumers) == 0 {
		return nil, fmt.Errorf("%s: pipeline %q has no consumers", src.Origin, src.Name)
	}

	transformers := make([]sdk.Transformer, 0)
	if err := drain(ctx, src.Transformers, lookup, func(b parse.Resource, instance sdk.Instance) {
		transformers = append(transformers, instance.Transform)
	}); err != nil {
		return nil, err
	}

	return &Pipeline{
		Producers:   producers,
		Consumers:   consumers,
		Transformer: stackTransform(transformers),
		logger:      logger,
		StopAfter:   src.StopAfter,
		ExitOnError: src.ExitOnError,
	}, nil
}
