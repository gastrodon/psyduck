package hcl

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"

	"github.com/gastrodon/psyduck/parse"
)

// resourceIndex resolves resource references against loaded plugins.
// References are either bare ("constant") or plugin-qualified
// ("psyduck.constant"). Bare references that match resources in more than
// one plugin are ambiguous and error with the candidate list.
type resourceIndex struct {
	plugins  map[string]sdk.Plugin
	byPlugin map[string]map[string]sdk.ResourceDescriptor
	owners   map[string][]string // resource name -> plugin names that provide it
}

func indexResources(plugins []sdk.Plugin) *resourceIndex {
	ix := &resourceIndex{
		plugins:  make(map[string]sdk.Plugin, len(plugins)),
		byPlugin: make(map[string]map[string]sdk.ResourceDescriptor, len(plugins)),
		owners:   make(map[string][]string),
	}

	for _, p := range plugins {
		ix.plugins[p.Name()] = p
		resources := make(map[string]sdk.ResourceDescriptor)
		for _, r := range p.Resources() {
			resources[r.Name] = r
			ix.owners[r.Name] = append(ix.owners[r.Name], p.Name())
		}
		ix.byPlugin[p.Name()] = resources
	}

	return ix
}

func (ix *resourceIndex) lookup(ref string) (string, sdk.ResourceDescriptor, error) {
	if plugin, resource, qualified := strings.Cut(ref, "."); qualified {
		resources, ok := ix.byPlugin[plugin]
		if !ok {
			return "", sdk.ResourceDescriptor{}, fmt.Errorf("unknown plugin %q in resource reference %q — is it declared in a plugin{} block? did you run `psyduck init`?", plugin, ref)
		}
		r, ok := resources[resource]
		if !ok {
			return "", sdk.ResourceDescriptor{}, fmt.Errorf("plugin %q has no resource %q", plugin, resource)
		}
		return plugin, r, nil
	}

	owners := ix.owners[ref]
	switch len(owners) {
	case 0:
		return "", sdk.ResourceDescriptor{}, fmt.Errorf("unknown resource %q — if it comes from a plugin, ensure the plugin is declared and run `psyduck init`", ref)
	case 1:
		return owners[0], ix.byPlugin[owners[0]][ref], nil
	default:
		sorted := append([]string(nil), owners...)
		sort.Strings(sorted)
		candidates := make([]string, len(sorted))
		for i, o := range sorted {
			candidates[i] = o + "." + ref
		}
		return "", sdk.ResourceDescriptor{}, fmt.Errorf(
			"resource %q is provided by more than one plugin — qualify it: %s",
			ref, strings.Join(candidates, ", "))
	}
}

// makeBinding turns one produce/consume/transform block into a
// parse.Resource: resource resolved, config block wrapped, meta decoded.
func makeBinding(block *hcl.Block, ix *resourceIndex, localsCtx *hcl.EvalContext) (parse.Resource, error) {
	resRef, name := block.Labels[0], block.Labels[1]
	origin := rangeOf(block.DefRange)

	pluginID, desc, err := ix.lookup(resRef)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s at %s: %w", block.Type, resRef, name, origin, err)
	}

	meta := sdk.BlockMeta{}
	metaBlock := &hclBlock{spec: metaSpec, body: block.Body, evalCtx: localsCtx, origin: origin}
	if err := metaBlock.Decode(&meta); err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: failed to decode meta: %w", block.Type, resRef, name, err)
	}

	return parse.Resource{
		Ref:      strings.Join([]string{block.Type, resRef, name}, "."),
		Kind:     verbKinds[block.Type],
		Resource: desc,
		PluginID: pluginID,
		Block:    &hclBlock{spec: desc.Spec, body: block.Body, evalCtx: localsCtx, origin: origin},
		Meta:     meta,
	}, nil
}

// ---------------------------------------------------------------------------
// Pipeline resolution
// ---------------------------------------------------------------------------

func evalRefList(attr *hcl.Attribute, ctx *hcl.EvalContext) ([]string, error) {
	v, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return nil, diags
	}

	converted, err := convert.Convert(v, cty.List(cty.String))
	if err != nil {
		return nil, fmt.Errorf("%s: expected a list of resource references: %w", attr.Range, err)
	}

	refs := make([]string, 0, converted.LengthInt())
	iter := converted.ElementIterator()
	for iter.Next() {
		_, ref := iter.Element()
		refs = append(refs, ref.AsString())
	}
	return refs, nil
}

func resolveRefs(pipeline string, refs []string, set map[string]parse.Resource) ([]parse.Resource, error) {
	resolved := make([]parse.Resource, len(refs))
	for i, ref := range refs {
		b, ok := set[ref]
		if !ok {
			return nil, fmt.Errorf("pipeline %q: unknown resource reference %q", pipeline, ref)
		}
		resolved[i] = b
	}
	return resolved, nil
}

func makePipeline(
	block *hcl.Block,
	bindings map[string]map[string]parse.Resource,
	refCtxs map[string]*hcl.EvalContext,
	localsCtx *hcl.EvalContext,
	ix *resourceIndex,
) (parse.Pipeline, error) {
	name := block.Labels[0]
	origin := rangeOf(block.DefRange)

	content, diags := block.Body.Content(pipelineSchema)
	if diags.HasErrors() {
		return parse.Pipeline{}, diags
	}

	pipe := parse.Pipeline{Name: name, Origin: origin}

	// consumers + transformers are always literal reference lists
	consRefs, err := evalRefList(content.Attributes["consume"], refCtxs[blockConsume])
	if err != nil {
		return parse.Pipeline{}, fmt.Errorf("pipeline %q: %w", name, err)
	}
	consumers, err := resolveRefs(name, consRefs, bindings[blockConsume])
	if err != nil {
		return parse.Pipeline{}, err
	}
	pipe.Consumers = parse.LiteralResourceFunc(consumers...)

	transformers := []parse.Resource{}
	if attr, ok := content.Attributes["transform"]; ok {
		transRefs, err := evalRefList(attr, refCtxs[blockTransform])
		if err != nil {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: %w", name, err)
		}
		if transformers, err = resolveRefs(name, transRefs, bindings[blockTransform]); err != nil {
			return parse.Pipeline{}, err
		}
	}
	pipe.Transformers = parse.LiteralResourceFunc(transformers...)

	// producers are either a literal list or a produce-from seed
	produceAttr, hasProduce := content.Attributes["produce"]
	remoteAttr, hasRemote := content.Attributes["produce-from"]
	switch {
	case hasProduce && hasRemote:
		return parse.Pipeline{}, fmt.Errorf("pipeline %q at %s: produce and produce-from are mutually exclusive", name, origin)
	case hasProduce:
		prodRefs, err := evalRefList(produceAttr, refCtxs[blockProduce])
		if err != nil {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: %w", name, err)
		}
		producers, err := resolveRefs(name, prodRefs, bindings[blockProduce])
		if err != nil {
			return parse.Pipeline{}, err
		}
		if len(producers) == 0 {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q at %s: at least one producer is required", name, origin)
		}
		pipe.Producers = parse.LiteralResourceFunc(producers...)
	case hasRemote:
		v, diags := remoteAttr.Expr.Value(refCtxs[blockProduce])
		if diags.HasErrors() {
			return parse.Pipeline{}, diags
		}
		seed, ok := bindings[blockProduce][v.AsString()]
		if !ok {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: unknown produce-from reference %q", name, v.AsString())
		}
		pipe.Producers = remoteBindings(seed, ix, localsCtx)
	default:
		return parse.Pipeline{}, fmt.Errorf("pipeline %q at %s: produce or produce-from is required", name, origin)
	}

	if attr, ok := content.Attributes["stop-after"]; ok {
		v, diags := attr.Expr.Value(localsCtx)
		if diags.HasErrors() {
			return parse.Pipeline{}, diags
		}
		converted, err := convert.Convert(v, cty.Number)
		if err != nil {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: stop-after: %w", name, err)
		}
		stopAfter, _ := converted.AsBigFloat().Int64()
		pipe.StopAfter = int(stopAfter)
	}

	if attr, ok := content.Attributes["exit-on-error"]; ok {
		v, diags := attr.Expr.Value(localsCtx)
		if diags.HasErrors() {
			return parse.Pipeline{}, diags
		}
		converted, err := convert.Convert(v, cty.Bool)
		if err != nil {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: exit-on-error: %w", name, err)
		}
		pipe.ExitOnError = converted.True()
	}

	return pipe, nil
}

// ---------------------------------------------------------------------------
// Dynamic producers (produce-from)
// ---------------------------------------------------------------------------

const remoteTimeout = 10 * time.Second

// remoteBindings hides a dynamic producer behind the ordinary Bindings
// stream: on first drain it binds and runs the seed producer, takes its
// first message, parses it as HCL produce blocks, and yields the resulting
// bindings. Core cannot tell these apart from literal producers.
//
// TODO stream: today only the first message is consumed, matching the old
// collectProducer behavior. A future revision can keep draining messages.
func remoteBindings(seed parse.Resource, ix *resourceIndex, localsCtx *hcl.EvalContext) parse.ResourceFunc {
	var yield parse.ResourceFunc
	return func(max int) ([]parse.Resource, error) {
		if yield == nil {
			bindings, err := drainSeed(seed, ix, localsCtx)
			if err != nil {
				return nil, err
			}
			yield = parse.LiteralResourceFunc(bindings...)
		}
		return yield(max)
	}
}

// drainSeed binds and runs the seed producer, takes its first message, and
// parses it as HCL produce blocks. Guarded by remoteTimeout.
func drainSeed(seed parse.Resource, ix *resourceIndex, localsCtx *hcl.EvalContext) ([]parse.Resource, error) {
	plugin, ok := ix.plugins[seed.PluginID]
	if !ok {
		return nil, fmt.Errorf("produce-from %s: plugin %q not loaded", seed.Ref, seed.PluginID)
	}

	instance, err := plugin.Bind(sdk.PRODUCER, seed.Resource.Name, seed.Block)
	if err != nil {
		return nil, fmt.Errorf("produce-from %s: failed to bind: %w", seed.Ref, err)
	}
	defer instance.Close()

	send, errs := make(chan []byte), make(chan error)
	go instance.Produce(send, errs)

	timeout := time.NewTimer(remoteTimeout)
	defer timeout.Stop()

	select {
	case <-timeout.C:
		return nil, fmt.Errorf("produce-from %s: timeout waiting for remote producer", seed.Ref)
	case err := <-errs:
		return nil, fmt.Errorf("produce-from %s: remote producer error: %w", seed.Ref, err)
	case msg := <-send:
		return parseRemoteProducers(seed.Ref, msg, ix, localsCtx)
	}
}

// parseRemoteProducers parses producer definitions received from a dynamic
// producer. The message is ordinary config; its origin names the remote.
func parseRemoteProducers(ref string, msg []byte, ix *resourceIndex, localsCtx *hcl.EvalContext) ([]parse.Resource, error) {
	file, diags := hclparse.NewParser().ParseHCL(msg, "remote://"+ref)
	if diags.HasErrors() {
		return nil, fmt.Errorf("produce-from %s: failed to parse remote config: %w", ref, diags)
	}

	content, diags := file.Body.Content(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: blockProduce, LabelNames: []string{"resource", "name"}}},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("produce-from %s: remote config: %w", ref, diags)
	}

	bindings := make([]parse.Resource, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		b, err := makeBinding(block, ix, localsCtx)
		if err != nil {
			return nil, fmt.Errorf("produce-from %s: %w", ref, err)
		}
		bindings = append(bindings, b)
	}

	return bindings, nil
}
