package configure

import (
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
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

/*
Given a literal hcl, parse out an eval ctx with all variables. Right now, this includes
`value.*` from `value {...}` blocks, and `env.*` from environment variables
*/
func makeEvalCtx(filename string, literal []byte, ctx *hcl.EvalContext) (*hcl.EvalContext, hcl.Diagnostics) {
	values, diags := ParseValuesDesc(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, diags
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"value": cty.ObjectVal(values),
			"env":   makeMapEnv(),
		},
	}, make(hcl.Diagnostics, 0)
}
