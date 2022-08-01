package core

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

func makeBodySchema(specMap sdk.SpecMap) *hcl.BodySchema {
	attributes := make([]hcl.AttributeSchema, len(specMap))

	index := 0
	for _, spec := range specMap {
		attributes[index] = hcl.AttributeSchema{
			Name:     spec.Name,
			Required: spec.Required,
		}

		index++
	}

	return &hcl.BodySchema{
		Attributes: attributes,
	}
}

func buildSpecMap(specMap sdk.SpecMap) hcldec.Spec {
	object := make(hcldec.ObjectSpec, len(specMap))
	for name, spec := range specMap {
		object[name] = buildSpec(spec)
	}

	return object
}

func buildSpec(spec *sdk.Spec) hcldec.Spec {
	hclSchema := &hcldec.AttrSpec{
		Name:     spec.Name,
		Type:     cty.Type(spec.Type),
		Required: spec.Required,
	}

	if spec.Required {
		return &hcldec.DefaultSpec{
			Primary: hclSchema,
			Default: &hcldec.LiteralSpec{
				Value: spec.Default,
			},
		}
	}

	return hclSchema
}
