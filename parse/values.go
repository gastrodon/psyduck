package parse

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

/*
Given a literal hcl, parse out an eval ctx with all variables. Right now, this includes
`value.*` from `value {...}` blocks, and `env.*` from environment variables
*/
func ParseValuesCtx(filename string, literal []byte, ctx *hcl.EvalContext) (*hcl.EvalContext, hcl.Diagnostics) {
	values, diags := ParseValuesDesc(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, diags
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"value": cty.ObjectVal(values),
			// "env":   makeMapEnv(),
		},
	}, make(hcl.Diagnostics, 0)
}

/*
For parsing values blocks
```

	values {
		foo = "bar"
		num = 123
		string_inspector = inspect(true)
	}

```
*/
func ParseValuesDesc(filename string, literal []byte, ctx *hcl.EvalContext) (map[string]cty.Value, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body `hcl:",remain"`
		Blocks   []struct {
			Entries map[string]cty.Value `hcl:",remain"`
		} `hcl:"value,block"`
	})

	if diags := gohcl.DecodeBody(file.Body, ctx, target); diags.HasErrors() {
		return nil, diags
	}

	l := 0
	for _, b := range target.Blocks {
		l += len(b.Entries)
	}

	values := make(map[string]cty.Value, l)
	for _, b := range target.Blocks {
		for key, value := range b.Entries {
			if _, ok := values[key]; ok {
				return nil, hcl.Diagnostics{{
					Severity:    hcl.DiagError,
					Summary:     "duplicate value " + key,
					Detail:      "More than one value named " + key + " in value blocks",
					Subject:     &hcl.Range{}, // TODO
					Context:     &hcl.Range{}, // TODO
					EvalContext: ctx,
				}}
			}

			values[key] = value
		}
	}

	return values, nil
}
