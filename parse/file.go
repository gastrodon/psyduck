package parse

import "github.com/hashicorp/hcl/v2"

type File interface {
	Pipelines(*hcl.EvalContext) (GroupDesc, hcl.Diagnostics)
}

func NewFile(filename string, literal []byte) File {
	return &fileBytes{filename, literal}
}

type fileBytes struct {
	filename string
	literal  []byte
}

func parentify(parent, child *hcl.EvalContext) *hcl.EvalContext {
	c := parent.NewChild()
	c.Functions = child.Functions
	c.Variables = child.Variables
	return c
}

// Parse the values and pipeline groups of a file, given a library context
func (f *fileBytes) Pipelines(baseCtx *hcl.EvalContext) (GroupDesc, hcl.Diagnostics) {
	ctx := parentify(&hcl.EvalContext{}, baseCtx)
	valuesCtx, diags := f.Values(ctx)
	if diags.HasErrors() {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "failed to parse values",
			Detail:      "failed to parse the values for this pipeline at " + f.filename,
			EvalContext: ctx,
		})
	}

	ctx = parentify(ctx, valuesCtx)
	pipelines, diags := f.Groups(ctx)
	if diags.HasErrors() {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "failed to parse pipeline",
			Detail:      "cound not parse pipelines components at " + f.filename,
			EvalContext: ctx,
		})
	}

	return pipelines, nil
}

type fileGroup struct {
	files []*fileBytes
}

func NewFileGroup(files map[string][]byte) File {
	i := 0
	fb := make([]*fileBytes, len(files))
	for filename, literal := range files {
		fb[i] = &fileBytes{filename, literal}
	}

	return &fileGroup{fb}
}

// Parse many files described as a map filename -> content
func (f *fileGroup) Pipelines(baseCtx *hcl.EvalContext) (GroupDesc, hcl.Diagnostics) {
	composed := make([]*PipelineDesc, 0)
	for _, file := range f.files {
		frag, diags := file.Pipelines(baseCtx)
		if diags.HasErrors() {
			return nil, diags.Append(&hcl.Diagnostic{
				Severity:    hcl.DiagError,
				Summary:     "failed to parse group member",
				Detail:      "failed to parse the pipeline literal group member at " + file.filename,
				EvalContext: baseCtx,
			})
		}

		composed = append(composed, frag...)
	}

	return composed, make(hcl.Diagnostics, 0)
}
