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

func MonifyGroup(frags []*PipelineDesc) *PipelineDesc {
	cRemoteProducer, cProduce, cConsume, cTransform := 0, 0, 0, 0
	for _, frag := range frags {
		cRemoteProducer += len(frag.RemoteProducers)
		cProduce += len(frag.Producers)
		cConsume += len(frag.Consumers)
		cTransform += len(frag.Transformers)
	}

	joined := &PipelineDesc{
		RemoteProducers: make([]*MoverDesc, cRemoteProducer),
		Producers:       make([]*MoverDesc, cProduce),
		Consumers:       make([]*MoverDesc, cConsume),
		Transformers:    make([]*MoverDesc, cTransform),
	}

	cRemoteProducer, cProduce, cConsume, cTransform = 0, 0, 0, 0
	for _, frag := range frags {
		for _, m := range frag.RemoteProducers {
			joined.RemoteProducers[cRemoteProducer] = m
			cRemoteProducer++
		}

		for _, m := range frag.Producers {
			joined.Producers[cProduce] = m
			cProduce++
		}

		for _, m := range frag.Consumers {
			joined.Consumers[cConsume] = m
			cConsume++
		}

		for _, m := range frag.Transformers {
			joined.Transformers[cTransform] = m
			cTransform++
		}
	}

	return joined
}

func Literal(filename string, literal []byte, baseCtx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
	ctx := parentify(&hcl.EvalContext{}, baseCtx)
	valuesCtx, diags := ParseValuesCtx(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "failed to parse values",
			Detail:      "failed to parse the values for this pipeline at " + filename,
			EvalContext: ctx,
		})
	}

	ctx = parentify(ctx, valuesCtx)
	pipelines, diags := ParsePipelinesDesc(filename, literal, ctx)
	if diags.HasErrors() {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "failed to parse pipeline",
			Detail:      "cound not parse pipelines components at " + filename,
			EvalContext: ctx,
		})
	}

	return pipelines, nil
}

func LiteralGroup(files map[string][]byte, baseCtx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
	composed := make([]*PipelineDesc, 0)
	for filename, literal := range files {
		frag, diags := Literal(filename, literal, baseCtx)
		if diags.HasErrors() {
			return nil, diags.Append(&hcl.Diagnostic{
				Severity:    hcl.DiagError,
				Summary:     "failed to parse group member",
				Detail:      "failed to parse the pipeline literal group member at " + filename,
				EvalContext: baseCtx,
			})
		}

		composed = append(composed, frag...)
	}

	return composed, make(hcl.Diagnostics, 0)
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
	Kind    string               `hcl:"resource,label" cty:"resource"`
	Options map[string]cty.Value `hcl:",remain" cty:"options"`
}

type PipelineOpts struct {
	StopAfter   int  `hcl:"stop-after,optional"`
	ExitOnError bool `hcl:"exit-on-error,optional"`
}

type PipelineDesc struct {
	Name            string       `hcl:"name,label"`
	RemoteProducers []*MoverDesc `hcl:"produce-from,block"`
	Producers       []*MoverDesc `hcl:"produce,block"`
	Consumers       []*MoverDesc `hcl:"consume,block"`
	Transformers    []*MoverDesc `hcl:"transform,block"`
	StopAfter       int          `hcl:"stop-after,optional"`
	ExitOnError     bool         `hcl:"exit-on-error,optional"` // TODO delete me
}

func ParsePipelinesDesc(filename string, literal []byte, ctx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	target := new(struct {
		hcl.Body `hcl:",remain"`
		Groups   []*PipelineDesc `hcl:"group,block"`
	})

	if diags := gohcl.DecodeBody(file.Body, ctx, target); diags.HasErrors() {
		return nil, diags
	}

	if len(target.Groups) == 0 {
		return make([]*PipelineDesc, 0), make(hcl.Diagnostics, 0)
	}

	return target.Groups, make(hcl.Diagnostics, 0)
}
