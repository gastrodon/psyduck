package parse

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

// Resource is a fully-parsed, not-yet-instantiated pipeline resource. It
// carries no callable plugin code — just enough to find the owning plugin
// and hand it the config block at Bind time:
//
//	instance, err := plugins[r.PluginID].Bind(r.Kind, r.Resource.Name, r.Block)
type Resource struct {
	Ref      string                 // qualified reference, e.g. "produce.constant.input"
	Kind     sdk.Kind               // the single kind this resource is used as
	Resource sdk.ResourceDescriptor // metadata for the plugin resource
	PluginID string                 // Name() of the owning plugin
	Block    sdk.ConfigBlock        // the resource's config block; Block.Origin() is where it was defined
	Meta     sdk.BlockMeta          // pre-decoded host-owned attributes
}

// ResourceFunc yields Resources in chunks of up to max until exhausted.
// A nil slice with nil error signals exhaustion. Draining may do real work
// (produce-from binds and runs its seed producer), so it takes the caller's
// context and must respect its cancellation and deadline. A produce-from
// stream may block in a call for as long as its seed stays quiet — callers
// that cannot wait must bound the call with ctx.
//
// Calling with max < 1 releases any resources held behind the stream (a
// produce-from seed producer is stopped and joined); it returns nil, nil and
// the stream is dead afterward. For literal streams it is a no-op.
type ResourceFunc func(ctx context.Context, max int) ([]Resource, error)

// LiteralResourceFunc wraps a fixed slice into a ResourceFunc that yields it in
// chunks and then exhausts.
func LiteralResourceFunc(resources ...Resource) ResourceFunc {
	pos := 0
	return func(_ context.Context, max int) ([]Resource, error) {
		if max < 1 || pos >= len(resources) {
			return nil, nil
		}
		end := min(pos+max, len(resources))
		chunk := resources[pos:end]
		pos = end
		return chunk, nil
	}
}

// Pipeline is a fully-resolved pipeline description. Each slot holds a
// ResourceFunc stream of parsed-but-not-instantiated resources. Dynamic
// producers (produce-from) are hidden inside the Producers stream by the
// Parser; Spec.RemoteSeed tells core the stream is live and may keep
// yielding new producers for as long as the seed keeps sending.
//
// ProduceFromParallel caps how many producers run concurrently at any
// moment. Zero (the default) runs everything as soon as it is known. A
// positive value runs producers in groups of at most that size; the next
// group starts only once the current group is exhausted.
type Pipeline struct {
	Name                string
	Origin              sdk.SourceRange
	Producers           ResourceFunc
	Consumers           ResourceFunc
	Transformers        ResourceFunc
	StopAfter           int
	ExitOnError         bool
	ProduceFromParallel int
	Spec                PipelineSpec
}

// PipelineSpec is display-only metadata describing the pipeline's declared
// resources. Reading it never instantiates anything, and a produce-from
// seed is never run. RemoteSeed doubles as core's signal that the Producers
// stream is dynamic: literal streams exhaust at build time, while a seeded
// stream must be drained lazily as the pipeline runs.
type PipelineSpec struct {
	Producers    []Resource // literal producers; empty when produce-from is used
	RemoteSeed   *Resource  // non-nil iff the pipeline uses produce-from
	Transformers []Resource
	Consumers    []Resource
}

// ConfigValues is an optional interface a ConfigBlock may implement to
// expose its evaluated attribute values, rendered as strings, for display.
type ConfigValues interface {
	Values() map[string]string
}
