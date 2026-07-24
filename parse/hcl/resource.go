package hcl

import (
	"fmt"
	"log"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
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
// parse.Resource. All config evaluation is eager: the body is checked
// strictly against the plugin spec plus this verb's metaSpecs entry (unknown
// attributes error), and every attribute is evaluated, defaulted, and
// converted here. Only the final decode into the plugin's struct is
// deferred to Bind time.
func makeBinding(block *hcl.Block, ix *resourceIndex, localsCtx *hcl.EvalContext) (parse.Resource, error) {
	resRef, name := block.Labels[0], block.Labels[1]
	origin := rangeOf(block.DefRange)

	pluginID, desc, err := ix.lookup(resRef)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s at %s: %w", block.Type, resRef, name, origin, err)
	}

	metaSpec := metaSpecs[block.Type]
	content, diags := block.Body.Content(blockSchema(desc.Spec, metaSpec))
	if diags.HasErrors() {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: %w", block.Type, resRef, name, diags)
	}

	metaVals, err := evalValues(blockMetaSpec, content.Attributes, localsCtx, origin)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: failed to decode meta: %w", block.Type, resRef, name, err)
	}
	meta := sdk.BlockMeta{}
	if err := (&hclBlock{values: metaVals, origin: origin}).Decode(&meta); err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: failed to decode meta: %w", block.Type, resRef, name, err)
	}

	specVals, err := evalValues(desc.Spec, content.Attributes, localsCtx, origin)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: %w", block.Type, resRef, name, err)
	}

	parallel, err := decodeParallel(content.Attributes, localsCtx, origin)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: %w", block.Type, resRef, name, err)
	}

	return parse.Resource{
		Ref:      strings.Join([]string{block.Type, resRef, name}, "."),
		Kind:     verbKinds[block.Type],
		Resource: desc,
		PluginID: pluginID,
		Block:    &hclBlock{values: specVals, origin: origin},
		Meta:     meta,
		Parallel: parallel,
	}, nil
}

// decodeParallel reads the core-only `parallel` meta attribute off a resource
// block. It is not part of sdk.BlockMeta (plugins never see it), so it is
// evaluated on its own rather than through the BlockMeta decode. Absent, it
// defaults to 1; present, it must be a whole number >= 1 — the value is a
// duplication count, so 0 (and any negative) is meaningless and rejected.
func decodeParallel(attrs hcl.Attributes, ctx *hcl.EvalContext, origin sdk.SourceRange) (int, error) {
	attr, ok := attrs[parallelSpec.Name]
	if !ok {
		return 1, nil
	}
	v, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return 0, diags
	}
	converted, err := convert.Convert(v, cty.Number)
	if err != nil {
		return 0, fmt.Errorf("%s: parallel: expected a whole number: %w", origin, err)
	}
	n, acc := converted.AsBigFloat().Int64()
	if acc != big.Exact {
		return 0, fmt.Errorf("%s: parallel: must be a whole number", origin)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: parallel: must be >= 1 (it duplicates the resource that many times); got %d", origin, n)
	}
	return int(n), nil
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

// expandParallel flattens each resource's host-owned parallel count into
// literal duplicates: a resource with Parallel = n appears n times in a row,
// exactly as if it had been listed n times. makeBinding guarantees Parallel
// >= 1, so this only ever grows the list. It is used for producers and
// consumers, whose runtimes already do the right thing with duplicates (the
// producer pool runs them, the sink fans out to each). Transformers are
// handled differently — see makePipeline — because chaining duplicate
// transformers would serialize them instead of running them in parallel.
func expandParallel(resources []parse.Resource) []parse.Resource {
	expanded := make([]parse.Resource, 0, len(resources))
	for _, r := range resources {
		for range r.Parallel {
			expanded = append(expanded, r)
		}
	}
	return expanded
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
	consumers = expandParallel(consumers)
	pipe.Consumers = parse.LiteralResourceFunc(consumers...)
	pipe.Spec.Consumers = consumers

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
	// Transformers are not expanded here the way producers and consumers are:
	// duplicating a transformer into the flat list would chain the copies (the
	// transform would run n times in series). Instead each transformer keeps
	// its Parallel count and core fans it out — n instances greedily sharing
	// one input — at the transform stage.
	pipe.Transformers = parse.LiteralResourceFunc(transformers...)
	pipe.Spec.Transformers = transformers

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
		producers = expandParallel(producers)
		pipe.Producers = parse.LiteralResourceFunc(producers...)
		pipe.Spec.Producers = producers
	case hasRemote:
		v, diags := remoteAttr.Expr.Value(refCtxs[blockProduce])
		if diags.HasErrors() {
			return parse.Pipeline{}, diags
		}
		seed, ok := bindings[blockProduce][v.AsString()]
		if !ok {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: unknown produce-from reference %q", name, v.AsString())
		}
		if seed.Parallel > 1 {
			// A produce-from seed is a single live stream that declares its
			// own producers over time; stamping out duplicate seeds would run
			// several competing listeners, not "the same producer n times".
			// Use produce-parallel to widen how many derived producers run.
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: parallel is not supported on a produce-from seed (%s); use produce-parallel to widen concurrency", name, seed.Ref)
		}
		timeout := remoteFirstMessageTimeout
		if attr, ok := content.Attributes["produce-from-timeout"]; ok {
			tv, diags := attr.Expr.Value(localsCtx)
			if diags.HasErrors() {
				return parse.Pipeline{}, diags
			}
			converted, err := convert.Convert(tv, cty.Number)
			if err != nil {
				return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-from-timeout: %w", name, err)
			}
			secs, acc := converted.AsBigFloat().Int64()
			if acc != big.Exact {
				return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-from-timeout: must be a whole number of seconds", name)
			}
			if secs < 0 {
				return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-from-timeout: must be non-negative", name)
			}
			if secs > math.MaxInt64/int64(time.Second) {
				return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-from-timeout: too large to represent as a duration", name)
			}
			timeout = time.Duration(secs) * time.Second
		}
		warn := func(msg string) { log.Printf("produce-from %s: %s", seed.Ref, msg) }
		pipe.Producers = remoteBindings(seed, ix, timeout, warn)
		pipe.Spec.RemoteSeed = &seed
	default:
		return parse.Pipeline{}, fmt.Errorf("pipeline %q at %s: produce or produce-from is required", name, origin)
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

	pipe.ProduceParallel = 1
	if attr, ok := content.Attributes["produce-parallel"]; ok {
		v, diags := attr.Expr.Value(localsCtx)
		if diags.HasErrors() {
			return parse.Pipeline{}, diags
		}
		converted, err := convert.Convert(v, cty.Number)
		if err != nil {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-parallel: %w", name, err)
		}
		n, acc := converted.AsBigFloat().Int64()
		if acc != big.Exact {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-parallel: must be a whole number", name)
		}
		switch {
		case n < 0:
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-parallel: must be non-negative", name)
		case n == 0 && hasRemote:
			// 0 means "run them all at once", which needs a known count.
			// produce-from has no fixed count, so 0 is meaningless there.
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-parallel: 0 (run all at once) requires a static produce list; produce-from has no fixed producer count", name)
		case n == 0:
			pipe.ProduceParallel = len(pipe.Spec.Producers)
		default:
			pipe.ProduceParallel = int(n)
		}
	}

	return pipe, nil
}
