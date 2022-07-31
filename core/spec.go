package core

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2/hcldec"
)

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
		Type:     spec.Type,
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
