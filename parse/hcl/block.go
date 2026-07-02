package hcl

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

// hclBlock implements sdk.ConfigBlock over an HCL body. It is the only
// place HCL types hide behind the sdk interface. Decode is spec-driven:
// only attributes named by spec are read, evaluated against the eval
// context, converted to the spec's type, and decoded into psy:-tagged
// structs via the gocty fork.
type hclBlock struct {
	spec    []*sdk.Spec
	body    hcl.Body
	evalCtx *hcl.EvalContext
	origin  sdk.SourceRange
}

func (b *hclBlock) Origin() sdk.SourceRange { return b.origin }

func (b *hclBlock) Decode(dst any) error {
	if len(b.spec) == 0 {
		return nil
	}

	schema := &hcl.BodySchema{Attributes: make([]hcl.AttributeSchema, len(b.spec))}
	for i, spec := range b.spec {
		schema.Attributes[i] = hcl.AttributeSchema{Name: spec.Name, Required: spec.Required}
	}

	content, _, diags := b.body.PartialContent(schema)
	if diags.HasErrors() {
		return diags
	}

	values := make(map[string]cty.Value, len(b.spec))
	for _, spec := range b.spec {
		want, err := specCty(spec)
		if err != nil {
			return fmt.Errorf("%s: bad spec for %s: %w", b.origin, spec.Name, err)
		}

		attr, ok := content.Attributes[spec.Name]
		if !ok {
			v, err := defaultVal(spec, want)
			if err != nil {
				return fmt.Errorf("%s: bad default for %s: %w", b.origin, spec.Name, err)
			}
			values[spec.Name] = v
			continue
		}

		v, diags := attr.Expr.Value(b.evalCtx)
		if diags.HasErrors() {
			return diags
		}

		if v.IsNull() {
			if spec.Required {
				return fmt.Errorf("%s: a value is required for %s", b.origin, spec.Name)
			}
			v, err := defaultVal(spec, want)
			if err != nil {
				return fmt.Errorf("%s: bad default for %s: %w", b.origin, spec.Name, err)
			}
			values[spec.Name] = v
			continue
		}

		converted, err := convert.Convert(v, want)
		if err != nil {
			return fmt.Errorf("%s: invalid value for %s (want %s): %w", b.origin, spec.Name, spec.Type, err)
		}
		values[spec.Name] = converted
	}

	if err := gocty.FromCtyValueTagged(cty.ObjectVal(values), dst, "psy"); err != nil {
		return fmt.Errorf("%s: failed to decode options: %w", b.origin, err)
	}

	return nil
}

// specCty translates the sdk's format-neutral type descriptor into cty.
func specCty(spec *sdk.Spec) (cty.Type, error) {
	switch spec.Type {
	case sdk.TypeString:
		return cty.String, nil
	case sdk.TypeInt, sdk.TypeFloat:
		return cty.Number, nil
	case sdk.TypeBool:
		return cty.Bool, nil
	case sdk.TypeList:
		if spec.ElemType == nil {
			return cty.NilType, fmt.Errorf("list spec %s has no ElemType", spec.Name)
		}
		elem, err := specCty(spec.ElemType)
		if err != nil {
			return cty.NilType, err
		}
		return cty.List(elem), nil
	case sdk.TypeMap:
		if spec.ElemType == nil {
			return cty.NilType, fmt.Errorf("map spec %s has no ElemType", spec.Name)
		}
		elem, err := specCty(spec.ElemType)
		if err != nil {
			return cty.NilType, err
		}
		return cty.Map(elem), nil
	case sdk.TypeObject:
		fields := make(map[string]cty.Type, len(spec.Fields))
		for _, f := range spec.Fields {
			t, err := specCty(f)
			if err != nil {
				return cty.NilType, err
			}
			fields[f.Name] = t
		}
		return cty.Object(fields), nil
	default:
		return cty.NilType, fmt.Errorf("unknown spec type %d", spec.Type)
	}
}

func defaultVal(spec *sdk.Spec, want cty.Type) (cty.Value, error) {
	if spec.Default == nil {
		return cty.NullVal(want), nil
	}
	return gocty.ToCtyValue(spec.Default, want)
}

// metaSpec drives decoding of host-owned attributes (sdk.BlockMeta) from
// resource blocks. Plugins never declare these.
var metaSpec = []*sdk.Spec{
	{Name: "per-minute", Description: "rate limit: items per minute (0 = unrestricted)", Type: sdk.TypeInt, Default: 0},
	{Name: "stop-after", Description: "stop after n items (0 = unrestricted)", Type: sdk.TypeInt, Default: 0},
}
