package configure

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

var (
	defaultCtx = new(hcl.EvalContext) // in case we want to have a place to put builtin functions
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
		hcl.Body `hcl:",remain"`
		Blocks   []PluginDesc `hcl:"plugin,block"`
	})
	if diags := gohcl.DecodeBody(file.Body, defaultCtx, target); diags.HasErrors() {
		return nil, diags
	}

	return target.Blocks, make(hcl.Diagnostics, 0)
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
					Detail:      "TODO",
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

type MoverDesc struct {
	Kind    string    `hcl:"resource" cty:"resource"`
	Options cty.Value `hcl:"options" cty:"options"`
}

type PipelineDesc struct {
	Name           string       `hcl:"name,label"`
	RemoteProducer *MoverDesc   `hcl:"produce-from,optional"`
	Producers      []*MoverDesc `hcl:"produce,optional"`
	Consumers      []*MoverDesc `hcl:"consume,optional"`
	Transformers   []*MoverDesc `hcl:"transform,optional"`
	StopAfter      int          `hcl:"stop-after,optional"`
	ExitOnError    bool         `hcl:"exit-on-error,optional"`
}

func ParsePipelinesDesc(filename string, literal []byte, ctx *hcl.EvalContext) (map[string]*PipelineDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body `hcl:",remain"`
		Blocks   []*PipelineDesc `hcl:"pipeline,block"`
	})

	if diags := gohcl.DecodeBody(file.Body, ctx, target); diags.HasErrors() {
		return nil, diags
	}

	if len(target.Blocks) == 0 {
		return make(map[string]*PipelineDesc, 0), nil
	}

	lookup := make(map[string]*PipelineDesc, len(target.Blocks))
	for _, desc := range target.Blocks {
		if _, ok := lookup[desc.Name]; ok {
			return nil, hcl.Diagnostics{{
				Severity:    0,
				Summary:     "duplicate pipeline key",
				Detail:      "The name " + desc.Name + " is a duplicate",
				EvalContext: ctx,
			}}
		}

		lookup[desc.Name] = desc
	}

	return lookup, make(hcl.Diagnostics, 0)
}
