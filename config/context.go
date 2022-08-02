package config

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

const NAMESPACE_VALUE = "value"

func makeContext(values *Values) (*hcl.EvalContext, error) {
	length := 0
	for _, block := range values.ValueBlocks {
		length += len(block.Entries)
	}

	valuesMap := make(map[string]cty.Value, length)
	for _, block := range values.ValueBlocks {
		for name, value := range block.Entries {
			valuesMap[name] = value
		}
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			NAMESPACE_VALUE: cty.MapVal(valuesMap),
		},
	}, nil
}
