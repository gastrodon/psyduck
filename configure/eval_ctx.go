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

func parseValueBlocks(filename string, literal []byte) (cty.Value, hcl.Diagnostics) {
	values := new(valueBlocks)
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}

	if diags := gohcl.DecodeBody(file.Body, nil, values); diags.HasErrors() {
		return cty.NilVal, diags
	}

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

	return cty.ObjectVal(valuesMap), nil
}

/*
Given a literal hcl, parse out an eval ctx with all variables. Right now, this includes
`value.*` from `value {...}` blocks, and `env.*` from environment variables
*/
func makeEvalCtx(filename string, literal []byte) (*hcl.EvalContext, hcl.Diagnostics) {
	values, diags := parseValueBlocks(filename, literal)
	if diags.HasErrors() {
		return nil, diags
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			NAMESPACE_VALUE: values,
			NAMESPACE_ENV:   makeMapEnv(),
		},
	}, make(hcl.Diagnostics, 0)
}
