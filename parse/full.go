package parse

import "github.com/hashicorp/hcl/v2"

func parentify(parent, child *hcl.EvalContext) *hcl.EvalContext {
	c := parent.NewChild()
	c.Functions = child.Functions
	c.Variables = child.Variables
	return c
}

// Parse the values and pipeline groups of a file, given a library context
func Parse(filename string, literal []byte, baseCtx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
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
	pipelines, diags := Groups(filename, literal, ctx)
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

// Parse many files described as a map filename -> content
func ParseMulti(files map[string][]byte, baseCtx *hcl.EvalContext) ([]*PipelineDesc, hcl.Diagnostics) {
	composed := make([]*PipelineDesc, 0)
	for filename, literal := range files {
		frag, diags := Parse(filename, literal, baseCtx)
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
