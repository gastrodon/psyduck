package parse

import "github.com/psyduck-etl/sdk"

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

// ResourceFunc yields ResourceFunc in chunks of up to max until exhausted.
// A nil slice with nil error signals exhaustion.
type ResourceFunc func(max int) ([]Resource, error)

// LiteralResourceFunc wraps a fixed slice into a ResourceFunc that yields it in
// chunks and then exhausts.
func LiteralResourceFunc(resources ...Resource) ResourceFunc {
	pos := 0
	return func(max int) ([]Resource, error) {
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
// Parser — core cannot tell them apart from literal ones.
type Pipeline struct {
	Name         string
	Origin       sdk.SourceRange
	Producers    ResourceFunc
	Consumers    ResourceFunc
	Transformers ResourceFunc
	StopAfter    int
	ExitOnError  bool
}
