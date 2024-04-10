package configure

import (
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

func makeMapEnv() cty.Value {
	env := os.Environ()
	envMap := make(map[string]cty.Value, len(env))
	for _, kv := range env {
		split := strings.Split(kv, "=")
		envMap[split[0]] = cty.StringVal(split[1])
	}

	return cty.ObjectVal(envMap)
}

func makeMapVal(values *valueBlocks) cty.Value {
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

	return cty.ObjectVal(valuesMap)
}

func loadValues(filename string, literal []byte) (*valueBlocks, hcl.Diagnostics) {
	target := new(valueBlocks)
	if file, diags := hclparse.NewParser().ParseHCL(literal, filename); diags != nil {
		return nil, diags
	} else {
		gohcl.DecodeBody(file.Body, nil, target)
		return target, nil
	}
}

func loadValuesContext(filename string, literal []byte) (*hcl.EvalContext, error) {
	if values, err := loadValues(filename, literal); err != nil {
		return nil, err
	} else {
		return &hcl.EvalContext{
			Variables: map[string]cty.Value{
				NAMESPACE_VALUE: makeMapVal(values),
				NAMESPACE_ENV:   makeMapEnv(),
			},
		}, nil
	}
}
