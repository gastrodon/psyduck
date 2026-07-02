package parse

import "github.com/psyduck-etl/sdk"

// Binding is a fully-parsed, not-yet-instantiated pipeline resource. It
// carries no callable plugin code — just enough to find the owning plugin
// and hand it the config block at Bind time:
//
//	instance, err := plugins[b.PluginID].Bind(b.Kind, b.Resource.Name, b.Block)
type Binding struct {
	Ref      string                 // qualified reference, e.g. "produce.constant.input"
	Kind     sdk.Kind               // the single kind this binding is used as
	Resource sdk.ResourceDescriptor // metadata for the plugin resource
	PluginID string                 // Name() of the owning plugin
	Block    sdk.ConfigBlock        // the resource's config block
	Meta     sdk.BlockMeta          // pre-decoded host-owned attributes
	Origin   sdk.SourceRange        // where the resource was defined
}

// Bindings yields Bindings in chunks of up to max until exhausted.
// A nil slice with nil error signals exhaustion.
type Bindings func(max int) ([]Binding, error)

// LiteralBindings wraps a fixed slice into a Bindings that yields it in
// chunks and then exhausts.
func LiteralBindings(bindings ...Binding) Bindings {
	pos := 0
	return func(max int) ([]Binding, error) {
		if max < 1 || pos >= len(bindings) {
			return nil, nil
		}
		end := min(pos+max, len(bindings))
		chunk := bindings[pos:end]
		pos = end
		return chunk, nil
	}
}

// Pipeline is a fully-resolved pipeline description. Each slot holds a
// Bindings stream of parsed-but-not-instantiated resources. Dynamic
// producers (produce-from) are hidden inside the Producers stream by the
// Format — core cannot tell them apart from literal ones.
type Pipeline struct {
	Name         string
	Origin       sdk.SourceRange
	Producers    Bindings
	Consumers    Bindings
	Transformers Bindings
	StopAfter    int
	ExitOnError  bool
}

// ErrNoValue reports a lookup miss by key.
type ErrNoValue struct {
	Key string
}

func (e *ErrNoValue) Error() string {
	return "no value for key: " + e.Key
}

// Result provides access to fully-resolved pipeline descriptions.
type Result interface {
	Pipeline(name string) (Pipeline, error)
	Pipelines() map[string]Pipeline
}

type result struct {
	pipelines map[string]Pipeline
}

func (r *result) Pipeline(name string) (Pipeline, error) {
	p, ok := r.pipelines[name]
	if !ok {
		return Pipeline{}, &ErrNoValue{Key: name}
	}
	return p, nil
}

func (r *result) Pipelines() map[string]Pipeline {
	return r.pipelines
}

// NewResult wraps resolved pipelines in a Result. Intended for Format
// implementations.
func NewResult(pipelines map[string]Pipeline) Result {
	return &result{pipelines: pipelines}
}
