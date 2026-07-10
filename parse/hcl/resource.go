package hcl

import (
	"context"
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
// parse.Resource. All config evaluation is eager: the body is checked
// strictly against the plugin spec + metaSpec (unknown attributes error),
// and every attribute is evaluated, defaulted, and converted here. Only
// the final decode into the plugin's struct is deferred to Bind time.
func makeBinding(block *hcl.Block, ix *resourceIndex, localsCtx *hcl.EvalContext) (parse.Resource, error) {
	resRef, name := block.Labels[0], block.Labels[1]
	origin := rangeOf(block.DefRange)

	pluginID, desc, err := ix.lookup(resRef)
	if err != nil {
		return parse.Resource{}, fmt.Errorf("%s %s.%s at %s: %w", block.Type, resRef, name, origin, err)
	}

	content, diags := block.Body.Content(blockSchema(desc.Spec))
	if diags.HasErrors() {
		return parse.Resource{}, fmt.Errorf("%s %s.%s: %w", block.Type, resRef, name, diags)
	}

	metaVals, err := evalValues(metaSpec, content.Attributes, localsCtx, origin)
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

	return parse.Resource{
		Ref:      strings.Join([]string{block.Type, resRef, name}, "."),
		Kind:     verbKinds[block.Type],
		Resource: desc,
		PluginID: pluginID,
		Block:    &hclBlock{values: specVals, origin: origin},
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
			secs, _ := converted.AsBigFloat().Int64()
			if secs < 0 {
				return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-from-timeout: must be non-negative", name)
			}
			timeout = time.Duration(secs) * time.Second
		}
		pipe.Producers = remoteBindings(seed, ix, localsCtx, timeout)
		pipe.Spec.RemoteSeed = &seed
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
		n, _ := converted.AsBigFloat().Int64()
		if n < 1 {
			return parse.Pipeline{}, fmt.Errorf("pipeline %q: produce-parallel: must be at least 1", name)
		}
		pipe.ProduceParallel = int(n)
	}

	return pipe, nil
}

// ---------------------------------------------------------------------------
// Dynamic producers (produce-from)
// ---------------------------------------------------------------------------

// remoteFirstMessageTimeout guards how long we wait for a seed producer to
// emit its first message. After the first message arrives the seed is
// considered alive and may stay quiet between messages for arbitrarily
// long — a socket-listener seed, for example. This is the default used
// when a pipeline doesn't set produce-from-timeout; the attribute (in
// seconds, 0 meaning wait indefinitely) overrides it per-pipeline.
const remoteFirstMessageTimeout = 10 * time.Second

// seedResult is one delivery from the seed producer: a parsed message's
// bindings (possibly empty), or an error terminating the stream.
type seedResult struct {
	bindings []parse.Resource
	err      error
}

// remoteBindings hides a dynamic producer behind the ordinary Bindings
// stream: the first drain binds and runs the seed producer, and every
// message the seed sends is parsed as HCL produce blocks and yielded as
// bindings. Core cannot tell these apart from literal producers, except
// that a call may block for as long as the seed stays quiet — waiting is
// bounded by the call's ctx, so callers that cannot wait must bring a
// deadline.
//
// The seed keeps running until it closes its send channel (natural
// exhaustion), fails, or the stream is torn down. The seed's lifetime is
// anchored to the ctx of the call that starts it (the run-time feeder's, in
// practice — see core.producerSource); any terminal return — exhaustion,
// error, a dead per-call ctx, or an explicit max < 1 release — stops the
// seed and joins its goroutine before returning, so no path leaves it
// running unobserved.
func remoteBindings(seed parse.Resource, ix *resourceIndex, localsCtx *hcl.EvalContext, timeout time.Duration) parse.ResourceFunc {
	var (
		results chan seedResult
		stop    context.CancelFunc
		buf     []parse.Resource
		done    bool
		gotAny  bool // at least one message arrived; see closed-without-sending below
	)

	// finish tears the stream down: cancel the seed's ctx and join runSeed
	// by draining results until it closes. Idempotent; a no-op if the seed
	// never started.
	finish := func() {
		done = true
		if stop == nil {
			return
		}
		stop()
		for range results {
		}
		stop = nil
	}

	return func(ctx context.Context, max int) ([]parse.Resource, error) {
		if max < 1 {
			finish()
			buf = nil
			return nil, nil
		}
		if done {
			if n := min(max, len(buf)); n > 0 {
				chunk := buf[:n]
				buf = buf[n:]
				return chunk, nil
			}
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			finish()
			return nil, fmt.Errorf("produce-from %s: waiting for remote producer: %w", seed.Ref, err)
		}

		if stop == nil {
			// first drain: the seed runs under its own ctx derived from this
			// call's, living across calls until finish or that ctx ends
			var seedCtx context.Context
			seedCtx, stop = context.WithCancel(ctx)
			results = make(chan seedResult)
			go runSeed(seedCtx, seed, ix, localsCtx, timeout, results)
		}

		for len(buf) == 0 {
			select {
			case <-ctx.Done():
				finish()
				return nil, fmt.Errorf("produce-from %s: waiting for remote producer: %w", seed.Ref, ctx.Err())
			case r, ok := <-results:
				if !ok {
					// Regression guard for #8: a seed that closes without
					// ever sending is an error, not an empty config.
					if !gotAny {
						finish()
						return nil, fmt.Errorf("produce-from %s: seed producer closed without sending", seed.Ref)
					}
					finish()
					return nil, nil
				}
				if r.err != nil {
					finish()
					return nil, r.err
				}
				gotAny = true
				buf = append(buf, r.bindings...)
			}
		}

		n := min(max, len(buf))
		chunk := buf[:n]
		buf = buf[n:]
		return chunk, nil
	}
}

// runSeed binds and runs the seed producer, parses every message it sends
// into produce bindings, and delivers them on out. It exits on ctx, on the
// first error, or when the seed closes its send channel — errors the seed
// emits between closing send and returning are still delivered, matching
// the engine's guarantee in core. Closes out on exit so the caller
// observes EOS. timeout guards the wait for the first message that
// actually declares producers — messages declaring none are skipped and
// leave the guard armed; 0 disables the guard. As everywhere else, a seed
// that ignores ctx can leak its own goroutine — but never runSeed itself,
// whose every send and receive also selects on ctx.
func runSeed(ctx context.Context, seed parse.Resource, ix *resourceIndex, localsCtx *hcl.EvalContext, timeout time.Duration, out chan<- seedResult) {
	defer close(out)

	deliver := func(r seedResult) bool {
		select {
		case out <- r:
			return true
		case <-ctx.Done():
			return false
		}
	}

	plugin, ok := ix.plugins[seed.PluginID]
	if !ok {
		deliver(seedResult{err: fmt.Errorf("produce-from %s: plugin %q not loaded", seed.Ref, seed.PluginID)})
		return
	}

	instance, err := plugin.Bind(sdk.PRODUCER, seed.Resource.Name, seed.Block)
	if err != nil {
		deliver(seedResult{err: fmt.Errorf("produce-from %s: failed to bind: %w", seed.Ref, err)})
		return
	}
	defer instance.Close()

	send, errs := make(chan []byte), make(chan error)
	returned := make(chan struct{}) // closed when the seed function returns
	go func() {
		defer close(returned)
		instance.Produce(ctx, send, errs)
	}()

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			deliver(seedResult{err: fmt.Errorf("produce-from %s: timeout waiting for remote producer", seed.Ref)})
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil // closed; stop selecting on it
				continue
			}
			if err != nil {
				deliver(seedResult{err: fmt.Errorf("produce-from %s: remote producer error: %w", seed.Ref, err)})
				return
			}
		case msg, ok := <-send:
			if !ok {
				// The seed finished. Errors sent after the data close are
				// still owed delivery: wait for the seed function to return
				// (after which no sender can exist) and drain errs.
				select {
				case <-returned:
				case <-ctx.Done():
					return
				}
				for {
					select {
					case err, ok := <-errs:
						if !ok {
							return
						}
						if err != nil {
							deliver(seedResult{err: fmt.Errorf("produce-from %s: remote producer error: %w", seed.Ref, err)})
							return
						}
					default:
						return
					}
				}
			}
			bindings, err := parseRemoteProducers(seed.Ref, msg, ix, localsCtx)
			if err != nil {
				deliver(seedResult{err: err})
				return
			}
			if len(bindings) == 0 {
				continue // nothing declared; the first-message guard stays armed
			}
			deadline = nil // the seed has yielded producers; it is alive
			if !deliver(seedResult{bindings: bindings}) {
				return
			}
		}
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

	// the env.* object was built from a prescan of local sources; remote
	// config may query env vars unseen there, so extend it
	localsCtx = extendEnv(localsCtx, envNames([]hcl.Body{file.Body}, nil))

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
