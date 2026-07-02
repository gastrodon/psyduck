package hcl

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/parse"

	"os"
)

const (
	nsValue = "value"
	nsEnv   = "env"
)

func envVal() cty.Value {
	env := os.Environ()
	envMap := make(map[string]cty.Value, len(env))
	for _, kv := range env {
		if idx := strings.Index(kv, "="); idx >= 0 {
			envMap[kv[:idx]] = cty.StringVal(kv[idx+1:])
		}
	}

	if len(envMap) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(envMap)
}

func objOrEmpty(m map[string]cty.Value) cty.Value {
	if len(m) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(m)
}

// makeValuesCtx merges all value {} blocks across sources (duplicate keys
// error) and returns the eval context exposing value.* and env.*.
func makeValuesCtx(blocks []*hcl.Block) (*hcl.EvalContext, error) {
	envCtx := &hcl.EvalContext{Variables: map[string]cty.Value{nsEnv: envVal()}}

	values := make(map[string]cty.Value)
	for _, block := range blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, diags
		}

		for name, attr := range attrs {
			if _, dup := values[name]; dup {
				return nil, fmt.Errorf("duplicate value key %q at %s", name, attr.Range)
			}

			v, diags := attr.Expr.Value(envCtx)
			if diags.HasErrors() {
				return nil, diags
			}
			values[name] = v
		}
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			nsValue: objOrEmpty(values),
			nsEnv:   envCtx.Variables[nsEnv],
		},
	}, nil
}

// refTree builds nested cty objects from dotted ref paths, so that the HCL
// expression `produce.constant.input` (or short-form `constant.input`)
// evaluates to the flat ref string "produce.constant.input".
type refTree map[string]any // string leaf | refTree branch

func (t refTree) insert(path []string, leaf string) error {
	head := path[0]
	if len(path) == 1 {
		if _, exists := t[head]; exists {
			return fmt.Errorf("ref conflict at %q", leaf)
		}
		t[head] = leaf
		return nil
	}

	child, ok := t[head]
	if !ok {
		child = refTree{}
		t[head] = child
	}

	branch, ok := child.(refTree)
	if !ok {
		return fmt.Errorf("ref conflict at %q", leaf)
	}
	return branch.insert(path[1:], leaf)
}

func (t refTree) value() cty.Value {
	out := make(map[string]cty.Value, len(t))
	for key, child := range t {
		switch c := child.(type) {
		case string:
			out[key] = cty.StringVal(c)
		case refTree:
			out[key] = c.value()
		}
	}
	return objOrEmpty(out)
}

// makeRefCtx builds the eval context used for one pipeline attribute. The
// verb determines which bindings are visible — this is how kind is inferred
// from context. Both the verb-qualified path (produce.constant.input) and
// the short form (constant.input) resolve; value.* and env.* stay available.
func makeRefCtx(verb string, bindings map[string]parse.Resource, valuesCtx *hcl.EvalContext) (*hcl.EvalContext, error) {
	tree := refTree{}
	for ref := range bindings {
		path := strings.Split(ref, ".")
		if err := tree.insert(path, ref); err != nil {
			return nil, fmt.Errorf("%s: %w", verb, err)
		}
		// short form drops the verb segment
		if err := tree.insert(path[1:], ref); err != nil {
			return nil, fmt.Errorf("%s: %w", verb, err)
		}
	}

	variables := make(map[string]cty.Value, len(tree)+2)
	for key, child := range tree {
		switch c := child.(type) {
		case string:
			variables[key] = cty.StringVal(c)
		case refTree:
			variables[key] = c.value()
		}
	}

	for name, v := range valuesCtx.Variables {
		if _, taken := variables[name]; taken {
			return nil, fmt.Errorf("resource namespace %q collides with reserved namespace", name)
		}
		variables[name] = v
	}

	return &hcl.EvalContext{Variables: variables}, nil
}

// rangeOf converts an hcl.Range to the sdk's format-agnostic SourceRange.
func rangeOf(r hcl.Range) sdk.SourceRange {
	return sdk.SourceRange{
		SourceName: r.Filename,
		StartLine:  r.Start.Line,
		StartCol:   r.Start.Column,
		EndLine:    r.End.Line,
		EndCol:     r.End.Column,
	}
}
