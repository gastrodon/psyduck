package configure

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

func parentify(parent, child *hcl.EvalContext) *hcl.EvalContext {
	c := parent.NewChild()
	c.Functions = child.Functions
	c.Variables = child.Variables
	return c
}

type pipelineParts struct {
	Producers    []*MoverDesc `hcl:"produce,block"`
	Consumers    []*MoverDesc `hcl:"consume,block"`
	Transformers []*MoverDesc `hcl:"transform,block"`
}

// TODO this should take a library.Ctx! it should look more like Literal
func Partial(filename string, literal []byte, context *hcl.EvalContext) (*pipelineParts, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	resources := new(pipelineParts)
	if diags := gohcl.DecodeBody(file.Body, context, resources); diags.HasErrors() {
		return nil, diags
	}

	return resources, nil
}

func Literal(filename string, literal []byte, baseCtx *hcl.EvalContext) (*PipelineDesc, error) {
	ctx := parentify(&hcl.EvalContext{}, baseCtx)
	valuesCtx, diags := ParseValuesCtx(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to load values ctx: %s", diags.Error())
	}

	ctx = parentify(ctx, valuesCtx)
	pipelines, diags := ParsePipelinesDesc(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "cound not parse pipelines descriptors",
			EvalContext: ctx,
		})
	}

	return pipelines, nil
}

func ReadDirectory(directory string) ([]byte, error) {
	literal := bytes.NewBuffer(nil)
	paths, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read files in %s: %s", directory, err)
	}

	for _, each := range paths {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".psy") {
			continue
		}

		if content, err := os.ReadFile(path.Join(directory, each.Name())); err != nil {
			return nil, fmt.Errorf("failed reading %s: %s", each.Name(), err)
		} else {
			literal.Write(content)
		}
	}

	return literal.Bytes(), nil
}

type MoverDesc struct {
	Kind    string    `hcl:"resource" cty:"resource"`
	Options cty.Value `hcl:"options" cty:"options"`
}

type PipelineDesc struct {
	Name              string       `hcl:"name,label"`
	RemoteProducer    *MoverDesc   `hcl:"produce-from,optional"`
	Producers         []*MoverDesc `hcl:"produce,optional"`
	Consumers         []*MoverDesc `hcl:"consume,optional"`
	Transformers      []*MoverDesc `hcl:"transform,optional"`
	StopAfter         int          `hcl:"stop-after,optional"`
	ExitOnError       bool         `hcl:"exit-on-error,optional"`
	ParallelProducers uint         `hcl:"parallel-producers"`
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
