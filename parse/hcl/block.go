package hcl

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// hclBlock implements sdk.ConfigBlock over an already-evaluated resource
// block. All HCL evaluation happens eagerly in makeBinding (via blockSchema
// + evalValues), so config errors surface at parse time; Decode only maps
// the finished cty values into the plugin's psy:-tagged struct.
type hclBlock struct {
	values map[string]cty.Value
	origin sdk.SourceRange
}

func (b *hclBlock) Origin() sdk.SourceRange { return b.origin }

func (b *hclBlock) Decode(dst any) error {
	if len(b.values) == 0 {
		return nil
	}
	if err := gocty.FromCtyValueTagged(cty.ObjectVal(b.values), dst, "psy"); err != nil {
		return fmt.Errorf("%s: failed to decode options: %w", b.origin, err)
	}
	return nil
}

// Values implements parse.ConfigValues: the evaluated attribute values
// rendered as display strings. Null values (absent, no default) are omitted.
func (b *hclBlock) Values() map[string]string {
	out := make(map[string]string, len(b.values))
	for name, v := range b.values {
		if v.IsNull() {
			continue
		}
		out[name] = renderValue(v)
	}
	return out
}

func renderValue(v cty.Value) string {
	if v.Type() == cty.String {
		return fmt.Sprintf("%q", v.AsString())
	}
	if converted, err := convert.Convert(v, cty.String); err == nil {
		return converted.AsString()
	}
	raw, err := ctyjson.Marshal(v, v.Type())
	if err != nil {
		return fmt.Sprintf("<unrenderable: %s>", err)
	}
	return string(raw)
}

// blockSchema is the exact schema for one resource block: the plugin's spec
// plus the host-owned metaSpec. Used with Content (not PartialContent) so
// unknown attributes error at parse time.
func blockSchema(spec []*sdk.Spec) *hcl.BodySchema {
	schema := &hcl.BodySchema{Attributes: make([]hcl.AttributeSchema, 0, len(spec)+len(metaSpec))}
	seen := make(map[string]bool, len(spec)+len(metaSpec))
	for _, group := range [][]*sdk.Spec{spec, metaSpec} {
		for _, s := range group {
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{Name: s.Name, Required: s.Required})
		}
	}
	return schema
}

// evalValues evaluates the attributes named by spec against the eval
// context, applying defaults and type conversion. Attribute presence and
// unknown-attribute rejection are the schema's job (blockSchema + Content);
// this only handles values.
func evalValues(spec []*sdk.Spec, attrs hcl.Attributes, evalCtx *hcl.EvalContext, origin sdk.SourceRange) (map[string]cty.Value, error) {
	values := make(map[string]cty.Value, len(spec))
	for _, s := range spec {
		want, err := specCty(s)
		if err != nil {
			return nil, fmt.Errorf("%s: bad spec for %s: %w", origin, s.Name, err)
		}

		attr, ok := attrs[s.Name]
		if !ok {
			v, err := defaultVal(s, want)
			if err != nil {
				return nil, fmt.Errorf("%s: bad default for %s: %w", origin, s.Name, err)
			}
			values[s.Name] = v
			continue
		}

		v, diags := attr.Expr.Value(evalCtx)
		if diags.HasErrors() {
			return nil, diags
		}

		if v.IsNull() {
			if s.Required {
				return nil, fmt.Errorf("%s: a value is required for %s", origin, s.Name)
			}
			v, err := defaultVal(s, want)
			if err != nil {
				return nil, fmt.Errorf("%s: bad default for %s: %w", origin, s.Name, err)
			}
			values[s.Name] = v
			continue
		}

		converted, err := convert.Convert(v, want)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid value for %s (want %s): %w", origin, s.Name, s.Type, err)
		}
		values[s.Name] = converted
	}
	return values, nil
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
