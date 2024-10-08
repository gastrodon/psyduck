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

type GroupDesc []*PipelineDesc

func (g GroupDesc) Filter(names []string) GroupDesc {
	t := make(map[string]struct{}, len(names))
	for _, name := range names {
		t[name] = struct{}{}
	}

	f := make(GroupDesc, len(g))
	i := 0
	for _, p := range g {
		if _, ok := t[p.Name]; ok {
			f[i] = p
			i++
		}
	}

	return f
}

func (g GroupDesc) Monify() *PipelineDesc {
	cRemoteProducer, cProduce, cConsume, cTransform := 0, 0, 0, 0
	for _, frag := range g {
		cRemoteProducer += len(frag.RemoteProducers)
		cProduce += len(frag.Producers)
		cConsume += len(frag.Consumers)
		cTransform += len(frag.Transformers)
	}

	joined := &PipelineDesc{
		RemoteProducers: make([]*MoverDesc, cRemoteProducer),
		Producers:       make([]*MoverDesc, cProduce),
		Consumers:       make([]*MoverDesc, cConsume),
		Transformers:    make([]*MoverDesc, cTransform),
	}

	cRemoteProducer, cProduce, cConsume, cTransform = 0, 0, 0, 0
	for _, frag := range g {
		for _, m := range frag.RemoteProducers {
			joined.RemoteProducers[cRemoteProducer] = m
			cRemoteProducer++
		}

		for _, m := range frag.Producers {
			joined.Producers[cProduce] = m
			cProduce++
		}

		for _, m := range frag.Consumers {
			joined.Consumers[cConsume] = m
			cConsume++
		}

		for _, m := range frag.Transformers {
			joined.Transformers[cTransform] = m
			cTransform++
		}
	}

	return joined
}

// Parse all of the group blocks
func (f fileBytes) Groups(ctx *hcl.EvalContext) (GroupDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(f.literal, f.filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body        `hcl:",remain"`
		Groups          GroupDesc    `hcl:"group,block"`
		RemoteProducers []*MoverDesc `hcl:"produce-from,block"`
		Producers       []*MoverDesc `hcl:"produce,block"`
		Consumers       []*MoverDesc `hcl:"consume,block"`
		Transformers    []*MoverDesc `hcl:"transform,block"`
	})

	if diags := gohcl.DecodeBody(file.Body, ctx, target); diags.HasErrors() {
		return nil, diags
	}

	rooted := GroupDesc{&PipelineDesc{
		RemoteProducers: target.RemoteProducers,
		Producers:       target.Producers,
		Consumers:       target.Consumers,
		Transformers:    target.Transformers,
	}}

	if len(target.Groups) == 0 {
		return rooted, make(hcl.Diagnostics, 0)
	}

	return append(rooted, target.Groups...), make(hcl.Diagnostics, 0)
}
