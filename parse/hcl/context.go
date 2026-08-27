package hcl

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/parse"
)

const (
	nsLocal   = "local"
	nsEnv     = "env"
	nsImports = "imports"
)

// envNames statically collects the env.* attribute names queried by any
// expression in the given bodies, before evaluation. Only traversals of
// the exact shape env.NAME count.
func envNames(bodies []hcl.Body, into map[string]bool) map[string]bool {
	if into == nil {
		into = map[string]bool{}
	}
	for _, body := range bodies {
		syn, ok := body.(*hclsyntax.Body)
		if !ok {
			continue // all our sources come through hclparse; non-syntax bodies have nothing to scan
		}
		for _, attr := range syn.Attributes {
			for _, traversal := range attr.Expr.Variables() {
				if traversal.RootName() != nsEnv || len(traversal) < 2 {
					continue
				}
				if step, ok := traversal[1].(hcl.TraverseAttr); ok {
					into[step.Name] = true
				}
			}
		}
		for _, block := range syn.Blocks {
			envNames([]hcl.Body{block.Body}, into)
		}
	}
	return into
}

// envVal builds the env.* object containing exactly the queried names.
// Unset variables resolve to "" rather than erroring.
func envVal(names map[string]bool) cty.Value {
	envMap := make(map[string]cty.Value, len(names))
	for name := range names {
		envMap[name] = cty.StringVal(os.Getenv(name))
	}
	return objOrEmpty(envMap)
}

func objOrEmpty(m map[string]cty.Value) cty.Value {
	if len(m) == 0 {
		return cty.EmptyObjectVal
	}
	return cty.ObjectVal(m)
}

// localRef tracks a local declaration: its name, attribute, and whether it's been resolved yet.
type localRef struct {
	name string
	attr *hcl.Attribute
	val  cty.Value
	set  bool
}

// makeLocalsCtx merges all locals {} blocks across sources (duplicate keys
// error) and returns the eval context exposing local.*, env.*, and
// imports.* (the resolved import{} closure for this file, built by the
// caller — see buildImportsValue in import.go).
//
// Locals are resolved to a fixed point via iterative evaluation: each pass
// evaluates unresolved locals against the current set of resolved locals plus
// env and imports. This allows locals to reference:
//  - env.* (environment variables)
//  - imports.* (imported resources and pipelines)
//  - local.* (other locals, forward and backward references)
func makeLocalsCtx(blocks []*hcl.Block, env cty.Value, imports cty.Value) (*hcl.EvalContext, error) {
	// Collect all local declarations first, checking for duplicates.
	locals := make(map[string]*localRef)
	allRefs := []*localRef{}
	for _, block := range blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, diags
		}

		for name, attr := range attrs {
			if _, dup := locals[name]; dup {
				return nil, fmt.Errorf("duplicate local %q at %s", name, attr.Range)
			}

			ref := &localRef{name: name, attr: attr}
			locals[name] = ref
			allRefs = append(allRefs, ref)
		}
	}

	// If there are no locals, we're done.
	if len(locals) == 0 {
		return &hcl.EvalContext{
			Variables: map[string]cty.Value{
				nsLocal:   objOrEmpty(make(map[string]cty.Value)),
				nsEnv:     env,
				nsImports: imports,
			},
		}, nil
	}

	// Resolve locals to a fixed point by iteratively evaluating unresolved
	// locals against the current set of resolved locals.
	for iterations := 0; iterations < len(locals)+1; iterations++ {
		resolved := make(map[string]cty.Value)
		for name, ref := range locals {
			if ref.set {
				resolved[name] = ref.val
			}
		}

		// Build eval context with current resolved locals, env, and imports.
		evalCtx := &hcl.EvalContext{
			Variables: map[string]cty.Value{
				nsLocal:   objOrEmpty(resolved),
				nsEnv:     env,
				nsImports: imports,
			},
		}

		progressMade := false
		unresolvedNames := []string{}
		for _, ref := range allRefs {
			if ref.set {
				continue // Already resolved
			}
			unresolvedNames = append(unresolvedNames, ref.name)

			v, diags := ref.attr.Expr.Value(evalCtx)
			if diags.HasErrors() {
				// Evaluation failed; check if this is due to undefined local refs.
				// If the error mentions an unknown variable or unsupported attribute
				// on local/imports objects, skip this local for now—maybe it will
				// resolve in the next iteration.
				errMsg := diags.Error()
				if strings.Contains(errMsg, "Unknown variable") ||
					strings.Contains(errMsg, "Unsupported attribute") {
					continue
				}
				// Other errors (type mismatches, etc.) are real and should fail immediately.
				return nil, diags
			}

			ref.val = v
			ref.set = true
			progressMade = true
		}

		// If all locals are now resolved, we're done.
		if len(unresolvedNames) == 0 {
			break
		}

		if !progressMade {
			// No progress was made this iteration; we're stuck with a circular dependency.
			return nil, fmt.Errorf("local resolution: circular dependency or undefined reference in: %v", unresolvedNames)
		}
	}

	// Final check: all locals should be resolved now.
	unresolved := []string{}
	for _, ref := range allRefs {
		if !ref.set {
			unresolved = append(unresolved, ref.name)
		}
	}
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("local resolution: unable to resolve: %v", unresolved)
	}

	// Check that all locals were resolved (shouldn't happen if progressMade logic is sound).
	values := make(map[string]cty.Value)
	for name, ref := range locals {
		if !ref.set {
			return nil, fmt.Errorf("local %q: undefined reference", name)
		}
		values[name] = ref.val
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			nsLocal:   objOrEmpty(values),
			nsEnv:     env,
			nsImports: imports,
		},
	}, nil
}

// refTree builds nested cty objects from dotted ref paths, so that the HCL
// expression `produce.constant.input` (or short-form `constant.input`)
// evaluates to the flat ref string "produce.constant.input". A leaf may be
// a plain string (auto-wrapped as cty.StringVal — the common case, a
// resource ref) or an already-built cty.Value (used for imports.* leaves
// that carry lists or scalars, e.g. imports.alias.pipeline.name.produce or
// .exit-on-error).
type refTree map[string]any // string | cty.Value | refTree

func (t refTree) insert(path []string, leaf any) error {
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

// vars flattens the tree into a variables map at the current level.
// String leaves become cty.StringVal, cty.Value leaves are used as-is,
// and branches recurse and become objects.
func (t refTree) vars() map[string]cty.Value {
	out := make(map[string]cty.Value, len(t))
	for key, child := range t {
		switch c := child.(type) {
		case string:
			out[key] = cty.StringVal(c)
		case cty.Value:
			out[key] = c
		case refTree:
			out[key] = c.value()
		}
	}
	return out
}

func (t refTree) value() cty.Value { return objOrEmpty(t.vars()) }

// makeRefCtx builds the eval context used for one pipeline attribute. The
// verb determines which bindings are visible — this is how kind is inferred
// from context. Both the verb-qualified path (produce.constant.input) and
// the short form (constant.input) resolve; local.* and env.* stay available.
func makeRefCtx(verb string, bindings map[string]parse.Resource, localsCtx *hcl.EvalContext) (*hcl.EvalContext, error) {
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

	variables := tree.vars()
	for name, v := range localsCtx.Variables {
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
