package configure

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

var (
	builtinCtx = new(hcl.EvalContext) // in case we want to have a place to put builtin functions
)

type PluginDesc struct {
	Name   string `hcl:"name,label"`
	Source string `hcl:"source"`
	Tag    string `hcl:"tag,optional"`
}

/*
For parsing plugin descriptor bocks
```

	plugin "name" {
		source = string
		tag 	 = string
	}

```
*/
func ParsePluginsDesc(filename string, literal []byte) ([]PluginDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		Blocks []PluginDesc `hcl:"plugin,block"`
	})
	if diags := gohcl.DecodeBody(file.Body, builtinCtx, target); diags.HasErrors() {
		return nil, diags
	}

	return target.Blocks, make(hcl.Diagnostics, 0)
}
