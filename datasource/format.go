package datasource

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/stdlib"
)

// Format bridges a configuration language (HCL, JSON, etc.) to the pipeline
// data datasource needs. A Format implementation parses raw config into the
// shapes expected here. Values() is internal to Format implementations —
// callers use Plugins(), Resources(), and Pipelines().
type Format interface {
	Plugins() ([]*sdk.Plugin, error)
	Values() (map[string]cty.Value, error)
	Resources([]*sdk.Plugin) (*ResourceSources, error)
	Pipelines() (map[string]PipelineDecl, error)
}

// ResourceSources holds the parsed resource bindings extracted by a Format.
// Each binding pairs a plugin resource factory with a format-agnostic parser
// closure. Consumed internally by Config.Datasources() and never exposed on Sources.
type ResourceSources struct {
	Producers    map[string]ResourceBinding
	Consumers    map[string]ResourceBinding
	Transformers map[string]ResourceBinding
}

// Sources provides access to fully-resolved pipeline descriptions.
// Each PipelineSource contains BindingSets of parsed-but-not-instantiated resources.
type Sources interface {
	Pipeline(name string) (PipelineSource, error)
	Pipelines() map[string]PipelineSource
}

type sources struct {
	pipelines map[string]PipelineSource
}

func (s *sources) Pipeline(name string) (PipelineSource, error) {
	p, ok := s.pipelines[name]
	if !ok {
		return PipelineSource{}, &ErrNoValue{Key: name}
	}
	return p, nil
}

func (s *sources) Pipelines() map[string]PipelineSource {
	return s.pipelines
}

// Config ties a Format to the datasource construction pipeline.
type Config[F Format] struct {
	Format F
}

// Datasources drives the Format through its phases and returns Sources
// containing fully-resolved PipelineSources keyed by pipeline name.
func (c *Config[F]) Datasources() (Sources, error) {
	plugins, err := c.Format.Plugins()
	if err != nil {
		return nil, err
	}

	allPlugins := append(plugins, stdlib.Plugin())

	resources, err := c.Format.Resources(allPlugins)
	if err != nil {
		return nil, err
	}

	decls, err := c.Format.Pipelines()
	if err != nil {
		return nil, err
	}

	resolved := make(map[string]PipelineSource, len(decls))
	for name, decl := range decls {
		prodBindings := make([]ResourceBinding, 0, len(decl.Producers))
		for _, key := range decl.Producers {
			b, ok := resources.Producers[key]
			if !ok {
				return nil, fmt.Errorf("pipeline %q: unknown producer resource %q", name, key)
			}
			prodBindings = append(prodBindings, b)
		}

		consBindings := make([]ResourceBinding, 0, len(decl.Consumers))
		for _, key := range decl.Consumers {
			b, ok := resources.Consumers[key]
			if !ok {
				return nil, fmt.Errorf("pipeline %q: unknown consumer resource %q", name, key)
			}
			consBindings = append(consBindings, b)
		}

		transBindings := make([]ResourceBinding, 0, len(decl.Transformers))
		for _, key := range decl.Transformers {
			b, ok := resources.Transformers[key]
			if !ok {
				return nil, fmt.Errorf("pipeline %q: unknown transformer resource %q", name, key)
			}
			transBindings = append(transBindings, b)
		}

		resolved[name] = PipelineSource{
			Name:         name,
			Producers:    LiteralBindingSet(prodBindings...),
			Consumers:    LiteralBindingSet(consBindings...),
			Transformers: LiteralBindingSet(transBindings...),
			StopAfter:    decl.StopAfter,
			ExitOnError:  decl.ExitOnError,
		}
	}

	return &sources{pipelines: resolved}, nil
}
