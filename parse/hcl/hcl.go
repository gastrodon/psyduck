// Package hcl implements parse.Parser for HCL-based .psy configuration.
// It is the only package that translates HCL/cty types into the
// format-agnostic sdk and parse types.
package hcl

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

const (
	blockLocals    = "locals"
	blockPlugin    = "plugin"
	blockImport    = "import"
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

var resourceVerbs = []string{blockProduce, blockConsume, blockTransform}

var topSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: blockLocals},
		{Type: blockPlugin, LabelNames: []string{"name"}},
		{Type: blockImport},
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
		{Name: "produce-parallel"},
		{Name: "produce-from-timeout"},
	},
}

// ParserHCL implements parse.Parser. It is stateless; Plugins/Parse take
// the entry file and a Loader to resolve import{} declarations with.
type ParserHCL struct{}

func NewParserHCL() *ParserHCL { return &ParserHCL{} }

// topBlocks is every top-level block in one file, bucketed by type.
// Resource blocks (produce/consume/transform) keep their block type.
type topBlocks struct {
	locals    []*hcl.Block
	plugins   []*hcl.Block
	imports   []*hcl.Block
	resources []*hcl.Block
	pipelines []*hcl.Block
}

// gatherOne parses one file's top-level blocks. Unlike the old workspace
// model, this never merges across files — cross-file sharing goes through
// import{} instead.
func gatherOne(src parse.Source) (*topBlocks, error) {
	parser := hclparse.NewParser()
	out := new(topBlocks)

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
		case blockImport:
			out.imports = append(out.imports, block)
		case blockProduce, blockConsume, blockTransform:
			out.resources = append(out.resources, block)
		case blockPipeline:
			out.pipelines = append(out.pipelines, block)
		}
	}

	return out, nil
}

// parsePluginSpec decodes one plugin{} block into a parse.Plugin.
func parsePluginSpec(block *hcl.Block) (parse.Plugin, error) {
	content, diags := block.Body.Content(pluginSchema)
	if diags.HasErrors() {
		return parse.Plugin{}, diags
	}

	spec := parse.Plugin{Name: block.Labels[0]}
	if attr, ok := content.Attributes["source"]; ok {
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return parse.Plugin{}, diags
		}
		spec.Source = v.AsString()
	}
	if attr, ok := content.Attributes["tag"]; ok {
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return parse.Plugin{}, diags
		}
		spec.Tag = v.AsString()
	}
	return spec, nil
}

// Plugins extracts every plugin{} declaration reachable from entry,
// following import{} blocks transitively. It's the cheap pre-pass: no
// plugins need to be loaded to run it.
func (h *ParserHCL) Plugins(entry string, load parse.Loader) ([]parse.Plugin, error) {
	var out []parse.Plugin
	err := collectPlugins(parse.ResolveImportPath("", entry), load, map[string]bool{}, map[string]bool{}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Parse resolves entry — and everything it transitively imports — against
// the given plugins, and returns every pipeline{} declared directly in
// entry. Resolution is pure evaluation today, so ctx is only checked on
// entry; produce-from seeds run later under the ctx their drain receives.
func (h *ParserHCL) Parse(ctx context.Context, entry string, load parse.Loader, plugins []sdk.Plugin) (map[string]parse.Pipeline, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := indexResources(plugins)
	result, err := resolveFile(parse.ResolveImportPath("", entry), load, index, map[string]bool{}, map[string]*fileResult{})
	if err != nil {
		return nil, err
	}
	return result.pipelines, nil
}

func bodiesOf(groups ...[]*hcl.Block) []hcl.Body {
	bodies := make([]hcl.Body, 0)
	for _, group := range groups {
		for _, block := range group {
			bodies = append(bodies, block.Body)
		}
	}
	return bodies
}
