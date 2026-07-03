// Package hcl implements parse.Parser for HCL-based .psy configuration.
// It is the only package that translates HCL/cty types into the
// format-agnostic sdk and parse types.
package hcl

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

const (
	blockLocals    = "locals"
	blockPlugin    = "plugin"
	blockProduce   = "produce"
	blockConsume   = "consume"
	blockTransform = "transform"
	blockPipeline  = "pipeline"
)

var verbKinds = map[string]sdk.Kind{
	blockProduce:   sdk.PRODUCER,
	blockConsume:   sdk.CONSUMER,
	blockTransform: sdk.TRANSFORMER,
}

var topSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: blockLocals},
		{Type: blockPlugin, LabelNames: []string{"name"}},
		{Type: blockProduce, LabelNames: []string{"resource", "name"}},
		{Type: blockConsume, LabelNames: []string{"resource", "name"}},
		{Type: blockTransform, LabelNames: []string{"resource", "name"}},
		{Type: blockPipeline, LabelNames: []string{"name"}},
	},
}

var pluginSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "source", Required: true},
		{Name: "tag"},
	},
}

var pipelineSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "produce"},
		{Name: "produce-from"},
		{Name: "consume", Required: true},
		{Name: "transform"},
		{Name: "stop-after"},
		{Name: "exit-on-error"},
	},
}

// ParserHCL implements parse.Parser. It is stateless; both phases take the
// sources they operate on.
type ParserHCL struct{}

func NewParserHCL() *ParserHCL { return &ParserHCL{} }

// topBlocks is every top-level block across all sources, bucketed by type.
// Resource blocks (produce/consume/transform) keep their block type.
type topBlocks struct {
	locals    []*hcl.Block
	plugins   []*hcl.Block
	resources []*hcl.Block
	pipelines []*hcl.Block
}

func gather(sources []parse.Source) (*topBlocks, error) {
	parser := hclparse.NewParser()
	out := new(topBlocks)

	for _, src := range sources {
		file, diags := parser.ParseHCL(src.Content, src.Name)
		if diags.HasErrors() {
			return nil, diags
		}

		content, diags := file.Body.Content(topSchema)
		if diags.HasErrors() {
			return nil, diags
		}

		for _, block := range content.Blocks {
			switch block.Type {
			case blockLocals:
				out.locals = append(out.locals, block)
			case blockPlugin:
				out.plugins = append(out.plugins, block)
			case blockProduce, blockConsume, blockTransform:
				out.resources = append(out.resources, block)
			case blockPipeline:
				out.pipelines = append(out.pipelines, block)
			}
		}
	}

	return out, nil
}

// Plugins is the cheap pre-pass: extract plugin {} declarations without
// needing any plugins loaded.
func (h *ParserHCL) Plugins(sources []parse.Source) ([]parse.Plugin, error) {
	blocks, err := gather(sources)
	if err != nil {
		return nil, err
	}

	specs := make([]parse.Plugin, 0, len(blocks.plugins))
	for _, block := range blocks.plugins {
		content, diags := block.Body.Content(pluginSchema)
		if diags.HasErrors() {
			return nil, diags
		}

		spec := parse.Plugin{Name: block.Labels[0]}
		if attr, ok := content.Attributes["source"]; ok {
			v, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				return nil, diags
			}
			spec.Source = v.AsString()
		}
		if attr, ok := content.Attributes["tag"]; ok {
			v, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				return nil, diags
			}
			spec.Tag = v.AsString()
		}

		specs = append(specs, spec)
	}

	return specs, nil
}

// Parse is the full pass: resolve values, resources, and pipelines against
// the loaded plugins and return format-agnostic pipeline descriptions.
func (h *ParserHCL) Parse(sources []parse.Source, plugins []sdk.Plugin) (map[string]parse.Pipeline, error) {
	blocks, err := gather(sources)
	if err != nil {
		return nil, err
	}

	localsCtx, err := makeLocalsCtx(blocks.locals)
	if err != nil {
		return nil, err
	}

	index := indexResources(plugins)

	bindings := map[string]map[string]parse.Resource{
		blockProduce:   {},
		blockConsume:   {},
		blockTransform: {},
	}
	for _, block := range blocks.resources {
		binding, err := makeBinding(block, index, localsCtx)
		if err != nil {
			return nil, err
		}
		if prev, dup := bindings[block.Type][binding.Ref]; dup {
			return nil, fmt.Errorf("duplicate resource %s at %s (previously defined at %s)",
				binding.Ref, binding.Block.Origin(), prev.Block.Origin())
		}
		bindings[block.Type][binding.Ref] = binding
	}

	refCtxs := make(map[string]*hcl.EvalContext, len(bindings))
	for verb, set := range bindings {
		ctx, err := makeRefCtx(verb, set, localsCtx)
		if err != nil {
			return nil, err
		}
		refCtxs[verb] = ctx
	}

	pipelines := make(map[string]parse.Pipeline, len(blocks.pipelines))
	for _, block := range blocks.pipelines {
		pipe, err := makePipeline(block, bindings, refCtxs, localsCtx, index)
		if err != nil {
			return nil, err
		}
		if prev, dup := pipelines[pipe.Name]; dup {
			return nil, fmt.Errorf("duplicate pipeline %q at %s (previously defined at %s)",
				pipe.Name, pipe.Origin, prev.Origin)
		}
		pipelines[pipe.Name] = pipe
	}

	return pipelines, nil
}
