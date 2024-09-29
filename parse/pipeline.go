package parse

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

type pipelineParts struct {
	Producers    []*MoverDesc `hcl:"produce,block"`
	Consumers    []*MoverDesc `hcl:"consume,block"`
	Transformers []*MoverDesc `hcl:"transform,block"`
}

// TODO this should take a library.Ctx! it should look more like Literal
// update: this should be swap-in replaceable with Literal
func Partial(filename string, literal []byte, context *hcl.EvalContext) (*pipelineParts, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	resources := new(pipelineParts)
	if diags := gohcl.DecodeBody(file.Body, context, resources); diags.HasErrors() {
		return nil, diags
	}

	return resources, nil
}

type MoverDesc struct {
	Kind    string               `hcl:"resource,label" cty:"resource"`
	Options map[string]cty.Value `hcl:",remain" cty:"options"`
}

type PipelineDesc struct {
	Name            string       `hcl:"name,label"`
	RemoteProducers []*MoverDesc `hcl:"produce-from,block"`
	Producers       []*MoverDesc `hcl:"produce,block"`
	Consumers       []*MoverDesc `hcl:"consume,block"`
	Transformers    []*MoverDesc `hcl:"transform,block"`
}

// Parse all of the group blocks
func Groups(filename string, literal []byte, ctx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body `hcl:",remain"`
		Groups   []*PipelineDesc `hcl:"group,block"`
	})

	if diags := gohcl.DecodeBody(file.Body, ctx, target); diags.HasErrors() {
		return nil, diags
	}

	if len(target.Groups) == 0 {
		return make([]*PipelineDesc, 0), make(hcl.Diagnostics, 0)
	}

	return target.Groups, make(hcl.Diagnostics, 0)
}
