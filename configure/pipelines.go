package configure

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

var pipelineBlockSpec = &hcldec.BlockObjectSpec{
	TypeName:   "pipeline",
	LabelNames: []string{"name"},

	Nested: hcldec.ObjectSpec{
		"produce": &hcldec.AttrSpec{
			Name:     "produce",
			Type:     cty.List(cty.String),
			Required: true,
		},
		"consume": &hcldec.AttrSpec{
			Name:     "consume",
			Type:     cty.List(cty.String),
			Required: true,
		},
		"transform": &hcldec.AttrSpec{
			Name:     "transform",
			Type:     cty.List(cty.String),
			Required: true,
		},
	},
}

func lookupRefSlice(refs []string, lookup map[string]*pipelinePart) ([]*pipelinePart, error) {
	resources := make([]*pipelinePart, len(refs))

	for index, ref := range refs {
		if resource, ok := lookup[ref]; !ok {
			return nil, fmt.Errorf("can't find a resource %s", ref)
		} else {
			resources[index] = resource
		}
	}

	return resources, nil
}

func lookupPipelines(refs map[string]*pipelineBlock, lookup map[string]*pipelinePart) (map[string]*Pipeline, error) {
	pipelines := make(map[string]*Pipeline, len(refs))
	for name, ref := range refs {
		producers, err := lookupRefSlice(ref.Producers, lookup)
		if err != nil {
			return nil, err
		}

		consumers, err := lookupRefSlice(ref.Consumers, lookup)
		if err != nil {
			return nil, err
		}

		transformers, err := lookupRefSlice(ref.Transformers, lookup)
		if err != nil {
			return nil, err
		}

		pipelines[name] = &Pipeline{
			Name:         name,
			Producers:    producers,
			Consumers:    consumers,
			Transformers: transformers,
		}
	}

	return pipelines, nil
}

func loadPipelines(filename string, literal []byte, context *hcl.EvalContext, lookup map[string]*pipelinePart) (map[string]*Pipeline, error) {
	if file, diags := hclparse.NewParser().ParseHCL(literal, filename); diags != nil {
		return nil, diags
	} else {
		if value, _, diags := hcldec.PartialDecode(file.Body, pipelineBlockSpec, context); diags != nil {
			return nil, diags
		} else {
			refs := make(map[string]*pipelineBlock, value.LengthInt())
			iter := value.ElementIterator()

			for iter.Next() {
				key, each := iter.Element()
				ref := new(pipelineBlock)
				if err := gocty.FromCtyValue(each, ref); err != nil {
					return nil, err
				} else {
					refs[key.AsString()] = ref
				}
			}

			return lookupPipelines(refs, lookup)
		}
	}
}
