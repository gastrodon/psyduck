package datasource

import "github.com/psyduck-etl/sdk"

// ResourceBinding is a fully-parsed, not-yet-instantiated pipeline resource.
// It pairs the plugin factory with a format-agnostic parser closure that
// knows how to decode the config block's options into the resource's config struct.
// Core calls binding.Resource.ProvideProducer(binding.Parse) (or Consumer/Transformer)
// to instantiate the resource.
type ResourceBinding struct {
	Kind     string        // qualified name: "produce.kind.name" (for error messages)
	Resource *sdk.Resource // plugin factory with ProvideProducer/Consumer/Transformer
	Parse    sdk.Parser    // closure capturing format-specific options
}

// BindingSet yields ResourceBindings in chunks until exhausted.
// Returns nil, nil to signal exhaustion. Same lazy-iteration pattern as ProducerSet.
type BindingSet func(max int) ([]ResourceBinding, error)

// LiteralBindingSet wraps a fixed slice of ResourceBindings into a BindingSet
// that yields them in chunks and exhausts when all have been returned.
func LiteralBindingSet(bindings ...ResourceBinding) BindingSet {
	pos := 0
	return func(max int) ([]ResourceBinding, error) {
		if max < 1 || pos >= len(bindings) {
			return nil, nil
		}
		end := pos + max
		if end > len(bindings) {
			end = len(bindings)
		}
		result := bindings[pos:end]
		pos = end
		return result, nil
	}
}

// PipelineDecl is the format-agnostic wiring declaration for a pipeline.
// Resources are referenced by their qualified string keys ("produce.kind.name").
// Used internally by Config.Datasources() to resolve bindings — not exposed on Sources.
type PipelineDecl struct {
	Name           string
	Producers      []string // qualified resource keys
	Consumers      []string
	Transformers   []string
	RemoteProducer string // resource key; mutually exclusive with Producers
	StopAfter      int
	ExitOnError    bool
}

// PipelineSource is a fully-resolved pipeline description.
// Each slot holds a BindingSet of parsed-but-not-instantiated resources.
// Core drains each BindingSet and instantiates resources by calling
// binding.Resource.ProvideProducer(binding.Parse) (or Consumer/Transformer).
type PipelineSource struct {
	Name         string
	Producers    BindingSet
	Consumers    BindingSet
	Transformers BindingSet
	StopAfter    int
	ExitOnError  bool
}
