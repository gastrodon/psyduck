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
			Required: false,
		},
		"produce-from": &hcldec.AttrSpec{
			Name:     "produce-from",
			Type:     cty.String,
			Required: false,
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
		"stop-after": &hcldec.AttrSpec{
			Name:     "stop-after",
			Type:     cty.Number,
			Required: false,
		},
		"parallel-producers": &hcldec.AttrSpec{
			Name:     "parallel-producers",
			Type:     cty.Number,
			Required: false,
		},
	},
}

func lookupRefSlice(refs []string, lookup map[string]*MoverDesc) ([]*MoverDesc, error) {
	resources := make([]*MoverDesc, len(refs))

	for index, ref := range refs {
		if resource, ok := lookup[ref]; !ok {
			return nil, fmt.Errorf("can't find a resource %s", ref)
		} else {
			resources[index] = resource
		}
	}

	return resources, nil
}

func derefOr[T any](v *T, d T) T {
	if v != nil {
		return *v
	}

	return d
}

func lookupPipelines(refs map[string]*pipelineBlock, lookup map[string]*MoverDesc) (map[string]*PipelineDesc, error) {
	pipelines := make(map[string]*PipelineDesc, len(refs))
	for name, ref := range refs {
		consumers, err := lookupRefSlice(ref.Consumers, lookup)
		if err != nil {
			return nil, fmt.Errorf("failed looking up consumer ref slice: %s", err)
		}

		transformers, err := lookupRefSlice(ref.Transformers, lookup)
		if err != nil {
			return nil, fmt.Errorf("failed looking up transformer ref slice: %s", err)
		}

		if ref.RemoteProducer != nil {
			r, ok := lookup[*ref.RemoteProducer]
			if !ok {
				return nil, fmt.Errorf("can't find a remote provider %s", *ref.RemoteProducer)
			}

			pipelines[name] = &PipelineDesc{
				Name:              name,
				RemoteProducer:    r,
				Producers:         nil,
				Consumers:         consumers,
				Transformers:      transformers,
				StopAfter:         derefOr(ref.StopAfter, 0),
				ExitOnError:       derefOr(ref.ExitOnError, false),
				ParallelProducers: derefOr(ref.ParallelProducers, 0),
			}
		} else {
			producers, err := lookupRefSlice(ref.Producers, lookup)
			if err != nil {
				return nil, fmt.Errorf("can't find a provider ref slice: %s", err)
			}

			pipelines[name] = &PipelineDesc{
				Name:              name,
				RemoteProducer:    nil,
				Producers:         producers,
				Consumers:         consumers,
				Transformers:      transformers,
				StopAfter:         derefOr(ref.StopAfter, 0),
				ExitOnError:       derefOr(ref.ExitOnError, false),
				ParallelProducers: derefOr(ref.ParallelProducers, 0),
			}
		}

	}

	return pipelines, nil
}

func loadPipelines(filename string, literal []byte, evalCtx *hcl.EvalContext, lookup map[string]*MoverDesc) (map[string]*PipelineDesc, error) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	value, _, diags := hcldec.PartialDecode(file.Body, pipelineBlockSpec, evalCtx)
	if diags.HasErrors() {
		return nil, diags
	}

	refs := make(map[string]*pipelineBlock, value.LengthInt())
	iter := value.ElementIterator()

	for iter.Next() {
		key, each := iter.Element()
		ref := new(pipelineBlock)
		if err := gocty.FromCtyValue(each, ref); err != nil {
			return nil, fmt.Errorf("failed to decode cty value: %s", err)
		} else {
			refs[key.AsString()] = ref
		}
	}

	return lookupPipelines(refs, lookup)
}
