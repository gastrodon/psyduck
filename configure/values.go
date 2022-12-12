package configure

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

func makeMapVal(values *Values) cty.Value {
	length := 0
	for _, block := range values.Blocks {
		length += len(block.Entries)
	}

	valuesMap := make(map[string]cty.Value, length)
	for _, block := range values.Blocks {
		for name, value := range block.Entries {
			valuesMap[name] = value
		}
	}

	if len(valuesMap) == 0 {
		return cty.MapValEmpty(cty.String)
	}

	return cty.MapVal(valuesMap)
}

func loadValues(filename string, literal []byte) (*Values, error) {
	values := new(Values)
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, nil, values)
	return values, nil
}

func loadValuesContext(filename string, literal []byte) (*hcl.EvalContext, error) {
	if values, err := loadValues(filename, literal); err != nil {
		return nil, err
	} else {
		return &hcl.EvalContext{
			Variables: map[string]cty.Value{
				NAMESPACE_VALUE: makeMapVal(values),
			},
		}, nil
	}
}
