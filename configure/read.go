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
)

func parentify(parent, child *hcl.EvalContext) *hcl.EvalContext {
	c := parent.NewChild()
	c.Functions = child.Functions
	c.Variables = child.Variables
	return c
}

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

func Literal(filename string, literal []byte, ctx *hcl.EvalContext) (map[string]*PipelineDesc, *hcl.EvalContext, error) {
	valuesCtx, diags := makeEvalCtx(filename, literal)
	if diags.HasErrors() {
		return nil, nil, fmt.Errorf("failed to load values ctx: %s", diags.Error())
	}

	pipelinesCtx := parentify(ctx, valuesCtx)
	pipelines, diags := ParsePipelinesDesc(filename, literal, pipelinesCtx)
	if diags.HasErrors() {
		return nil, nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "cound not parse pipelines descriptors",
			EvalContext: pipelinesCtx,
		})
	}

	return pipelines, valuesCtx, nil
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
