package hcl

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/parse"
)

// resolveImportAttrs merges the attributes of every import{} block in a
// file (duplicate alias errors, mirroring makeLocalsCtx) and evaluates
// each path expression against ctx, returning alias -> path string.
// Import path expressions only see env.* — not local.* or imports.* from
// other imports — so import resolution never has to wait on the rest of
// the file's own eval context.
func resolveImportAttrs(blocks []*hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	out := make(map[string]string)
	for _, block := range blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, diags
		}
		for name, attr := range attrs {
			if _, dup := out[name]; dup {
				return nil, fmt.Errorf("duplicate import %q at %s", name, attr.Range)
			}
			v, diags := attr.Expr.Value(ctx)
			if diags.HasErrors() {
				return nil, diags
			}
			str, err := ctyConvertString(v)
			if err != nil {
				return nil, fmt.Errorf("import %q at %s: %w", name, attr.Range, err)
			}
			out[name] = str.AsString()
		}
	}
	return out, nil
}

func ctyConvertString(v cty.Value) (cty.Value, error) {
	if v.IsNull() {
		return cty.NilVal, fmt.Errorf("import path must not be null")
	}
	if v.Type() == cty.String {
		return v, nil
	}
	return cty.Value{}, fmt.Errorf("import path must be a string, got %s", v.Type().FriendlyName())
}

// importEnvCtx builds the minimal env.*-only eval context used to resolve
// import{} path expressions in a file.
func importEnvCtx(imports []*hcl.Block) *hcl.EvalContext {
	env := envVal(envNames(bodiesOf(imports), nil))
	return &hcl.EvalContext{Variables: map[string]cty.Value{nsEnv: env}}
}

// fileResult is what resolving one file (its own blocks, with its own
// imports already folded in) produces: its own resource bindings —
// exposed to importers under imports.<alias> — and its own pipeline{}
// blocks. Imports are not transitive: importing a file exposes only that
// file's own declarations, not whatever it in turn imported.
type fileResult struct {
	bindings  map[string]map[string]parse.Resource // verb -> ref -> Resource, this file's own
	pipelines map[string]parse.Pipeline            // this file's own pipeline{} blocks
}

// collectPlugins walks entry and its transitive imports, collecting every
// plugin{} declaration. It shares path resolution and cycle detection
// with resolveFile but doesn't need to resolve resources or pipelines.
func collectPlugins(path string, load parse.Loader, visiting, seen map[string]bool, out *[]parse.Plugin) error {
	if seen[path] {
		return nil
	}
	if visiting[path] {
		return fmt.Errorf("import cycle detected at %s", path)
	}
	visiting[path] = true
	defer delete(visiting, path)

	src, err := load(path)
	if err != nil {
		return err
	}
	blocks, err := gatherOne(src)
	if err != nil {
		return err
	}

	for _, block := range blocks.plugins {
		spec, err := parsePluginSpec(block)
		if err != nil {
			return err
		}
		*out = append(*out, spec)
	}

	aliases, err := resolveImportAttrs(blocks.imports, importEnvCtx(blocks.imports))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for alias, importPath := range aliases {
		childPath := parse.ResolveImportPath(path, importPath)
		if err := collectPlugins(childPath, load, visiting, seen, out); err != nil {
			return fmt.Errorf("import %q at %s: %w", alias, path, err)
		}
	}

	seen[path] = true
	return nil
}

// resolveFile fully resolves one file: its own env/locals, its imports
// (recursively, each producing a fileResult folded into this file's
// imports.* namespace), its own resources, and its own pipelines.
func resolveFile(
	path string,
	load parse.Loader,
	index *resourceIndex,
	visiting map[string]bool,
	cache map[string]*fileResult,
) (*fileResult, error) {
	if cached, ok := cache[path]; ok {
		return cached, nil
	}
	if visiting[path] {
		return nil, fmt.Errorf("import cycle detected at %s", path)
	}
	visiting[path] = true
	defer delete(visiting, path)

	src, err := load(path)
	if err != nil {
		return nil, err
	}
	blocks, err := gatherOne(src)
	if err != nil {
		return nil, err
	}

	// Imports first: path expressions only see env.*, so this never
	// depends on the rest of this file's own resolution.
	importAliases, err := resolveImportAttrs(blocks.imports, importEnvCtx(blocks.imports))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	importResults := make(map[string]*fileResult, len(importAliases))
	for alias, importPath := range importAliases {
		childPath := parse.ResolveImportPath(path, importPath)
		child, err := resolveFile(childPath, load, index, visiting, cache)
		if err != nil {
			return nil, fmt.Errorf("import %q at %s: %w", alias, path, err)
		}
		importResults[alias] = child
	}

	importsVal, err := buildImportsValue(importResults)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	env := envVal(envNames(bodiesOf(blocks.locals, blocks.resources, blocks.pipelines), nil))
	localsCtx, err := makeLocalsCtx(blocks.locals, env, importsVal)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	// This file's own resources, keyed by their plain (unqualified) ref —
	// what gets exposed to importers, and what local pipeline{} blocks in
	// this file see via the short/verb-qualified form.
	own := map[string]map[string]parse.Resource{
		blockProduce:   {},
		blockConsume:   {},
		blockTransform: {},
	}
	for _, block := range blocks.resources {
		binding, err := makeBinding(block, index, localsCtx)
		if err != nil {
			return nil, err
		}
		if prev, dup := own[block.Type][binding.Ref]; dup {
			return nil, fmt.Errorf("duplicate resource %s at %s (previously defined at %s)",
				binding.Ref, binding.Block.Origin(), prev.Block.Origin())
		}
		own[block.Type][binding.Ref] = binding
	}

	// refCtxs are built from local bindings only — imports.* is already
	// visible via localsCtx (merged into every verb's context below).
	refCtxs := make(map[string]*hcl.EvalContext, len(resourceVerbs))
	for _, verb := range resourceVerbs {
		ctx, err := makeRefCtx(verb, own[verb], localsCtx)
		if err != nil {
			return nil, err
		}
		refCtxs[verb] = ctx
	}

	// lookupBindings is what resolveRefs actually searches: this file's
	// own resources plus every imported resource under its qualified
	// imports.<alias>.<ref> key, matching exactly the string an
	// imports.<alias>... expression evaluates to. Resources reachable only
	// through an imported pipeline's produce/consume/transform list (which
	// may in turn have come from *that* file's own imports — no
	// transitive re-export of plain bindings) get a synthetic
	// imports.<alias>.pipeline.<name>.<verb>[i] key instead, matching
	// pipelineSlotKey/refListVal below.
	lookupBindings := map[string]map[string]parse.Resource{
		blockProduce:   cloneResourceMap(own[blockProduce]),
		blockConsume:   cloneResourceMap(own[blockConsume]),
		blockTransform: cloneResourceMap(own[blockTransform]),
	}
	for alias, child := range importResults {
		for verb, set := range child.bindings {
			for ref, res := range set {
				lookupBindings[verb]["imports."+alias+"."+ref] = res
			}
		}
		for pname, pipe := range child.pipelines {
			for _, slot := range pipelineSlots(pipe) {
				for i, res := range slot.resources {
					lookupBindings[slot.verb][pipelineSlotKey(alias, pname, slot.verb, i)] = res
				}
			}
		}
	}

	pipelines := make(map[string]parse.Pipeline, len(blocks.pipelines))
	for _, block := range blocks.pipelines {
		pipe, err := makePipeline(block, lookupBindings, refCtxs, localsCtx, index)
		if err != nil {
			return nil, err
		}
		if prev, dup := pipelines[pipe.Name]; dup {
			return nil, fmt.Errorf("duplicate pipeline %q at %s (previously defined at %s)",
				pipe.Name, pipe.Origin, prev.Origin)
		}
		pipelines[pipe.Name] = pipe
	}

	result := &fileResult{bindings: own, pipelines: pipelines}
	cache[path] = result
	return result, nil
}

func cloneResourceMap(m map[string]parse.Resource) map[string]parse.Resource {
	out := make(map[string]parse.Resource, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// buildImportsValue builds the imports.* object exposed to a file's own
// eval context: one key per alias, shaped as
// imports.<alias>.{produce,consume,transform}.<kind>.<name> for resources
// (or ...<plugin>.<kind>.<name> when the resource's own ref was
// plugin-qualified — imports mirror whatever qualification the imported
// file itself used, same as its own local refs) and
// imports.<alias>.pipeline.<name>.{produce,consume,transform,
// exit-on-error} for that file's pipeline{} blocks.
func buildImportsValue(results map[string]*fileResult) (cty.Value, error) {
	imports := refTree{}
	for alias, r := range results {
		aliasTree := refTree{
			blockProduce:   refTree{},
			blockConsume:   refTree{},
			blockTransform: refTree{},
		}
		for _, verb := range resourceVerbs {
			verbTree := aliasTree[verb].(refTree)
			for ref := range r.bindings[verb] {
				// ref is "<verb>.<kind>.<name>" or, when the resource was
				// declared plugin-qualified, "<verb>.<plugin>.<kind>.<name>".
				// Either way, dropping just the verb segment gives the
				// right nested path.
				rest := strings.SplitN(ref, ".", 2)
				if len(rest) != 2 {
					return cty.NilVal, fmt.Errorf("malformed resource ref %q", ref)
				}
				if err := verbTree.insert(strings.Split(rest[1], "."), "imports."+alias+"."+ref); err != nil {
					return cty.NilVal, fmt.Errorf("imports.%s: %w", alias, err)
				}
			}
		}

		pipelineTree := refTree{}
		for pname, pipe := range r.pipelines {
			slot := refTree{
				"exit-on-error": cty.BoolVal(pipe.ExitOnError),
			}
			for _, s := range pipelineSlots(pipe) {
				slot[s.verb] = refListVal(alias, pname, s.verb, s.resources)
			}
			pipelineTree[pname] = slot
		}
		aliasTree["pipeline"] = pipelineTree

		imports[alias] = aliasTree
	}
	return imports.value(), nil
}

// pipelineResourceSlot pairs one produce/consume/transform slot of a
// pipeline{} with its resolved resources, for iterating all three
// uniformly.
type pipelineResourceSlot struct {
	verb      string
	resources []parse.Resource
}

func pipelineSlots(pipe parse.Pipeline) []pipelineResourceSlot {
	return []pipelineResourceSlot{
		{blockProduce, pipe.Spec.Producers},
		{blockConsume, pipe.Spec.Consumers},
		{blockTransform, pipe.Spec.Transformers},
	}
}

// pipelineSlotKey is the synthetic lookup key for the i'th resource in one
// imported pipeline's produce/consume/transform slot. It's synthetic
// (rather than reusing the resource's own Ref) because that resource may
// itself have come from a further import inside the imported file — Refs
// don't carry that provenance, and plain bindings aren't transitively
// re-exported, so a fresh, always-reachable key is needed here. Used both
// as the list element strings imports.<alias>.pipeline.<name>.<verb>
// evaluates to (buildImportsValue) and as the matching lookupBindings key
// (resolveFile), so the two always agree.
func pipelineSlotKey(alias, pname, verb string, index int) string {
	return fmt.Sprintf("imports.%s.pipeline.%s.%s[%d]", alias, pname, verb, index)
}

// refListVal builds the ordered list of qualified refs for one
// pipeline{}'s producer/consumer/transformer slot, as exposed through
// imports.<alias>.pipeline.<name>.<verb>.
func refListVal(alias, pname, verb string, resources []parse.Resource) cty.Value {
	if len(resources) == 0 {
		return cty.ListValEmpty(cty.String)
	}
	vals := make([]cty.Value, len(resources))
	for i := range resources {
		vals[i] = cty.StringVal(pipelineSlotKey(alias, pname, verb, i))
	}
	return cty.ListVal(vals)
}
