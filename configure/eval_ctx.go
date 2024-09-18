package configure

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

/*
Given a literal hcl, parse out an eval ctx with all variables. Right now, this includes
`value.*` from `value {...}` blocks, and `env.*` from environment variables
*/
func makeEvalCtx(filename string, literal []byte) (*hcl.EvalContext, hcl.Diagnostics) {
	values, diags := ParseValuesDesc(filename, literal)
	if diags.HasErrors() {
		return nil, diags
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			NAMESPACE_VALUE: cty.ObjectVal(values),
		},
	}, make(hcl.Diagnostics, 0)
}
