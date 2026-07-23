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

// Encode renders the evaluated attribute values as a JSON object so the
// block can cross a process boundary (sdk/rpc sends this to plugin
// subprocesses, which rebuild the block with sdk.NewJSONBlock). Null
// values (absent, no default) are kept as JSON nulls; the JSON decoder on
// the far side treats them as absent.
func (b *hclBlock) Encode() ([]byte, error) {
	if len(b.values) == 0 {
		return []byte("{}"), nil
	}
	obj := cty.ObjectVal(b.values)
	raw, err := ctyjson.Marshal(obj, obj.Type())
	if err != nil {
		return nil, fmt.Errorf("%s: failed to encode options: %w", b.origin, err)
	}
	return raw, nil
}

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
// plus its verb's host-owned meta attributes (see metaSpecs). Used with
// Content (not PartialContent) so unknown attributes error at parse time —
// a meta attribute unsupported by this verb (e.g. stop-after on a consume
// block) is rejected the same way an unknown plugin attribute is.
func blockSchema(spec []*sdk.Spec, meta []*sdk.Spec) *hcl.BodySchema {
	schema := &hcl.BodySchema{Attributes: make([]hcl.AttributeSchema, 0, len(spec)+len(meta))}
	seen := make(map[string]bool, len(spec)+len(meta))
	for _, group := range [][]*sdk.Spec{spec, meta} {
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

var (
	perMinuteSpec = &sdk.Spec{Name: "per-minute", Description: "rate limit: items per minute (0 = unrestricted)", Type: sdk.TypeInt, Default: 0}
	stopAfterSpec = &sdk.Spec{Name: "stop-after", Description: "stop after n items (0 = unrestricted)", Type: sdk.TypeInt, Default: 0}
	parallelSpec  = &sdk.Spec{Name: "parallel", Description: "duplicate this resource n times (default 1, must be >= 1)", Type: sdk.TypeInt, Default: 1}
)

// metaSpecs drives which host-owned attributes a block's source may set,
// keyed by verb. Plugins never declare these. stop-after is a producer-only
// flow governor — accepting it on consume or transform would silently do
// nothing (nothing wraps a transformer, and a consumer's own completion is
// its own to decide), so those verbs simply don't offer it. per-minute paces
// both producers and consumers. parallel is accepted on every verb: it just
// stamps out copies of the block, which is meaningful for any kind. Note
// that per-minute and stop-after belong to sdk.BlockMeta (decoded via
// blockMetaSpec), while parallel is core-only (decoded straight into
// parse.Resource.Parallel by makeBinding) — so it is deliberately absent from
// blockMetaSpec.
var metaSpecs = map[string][]*sdk.Spec{
	blockProduce:   {perMinuteSpec, stopAfterSpec, parallelSpec},
	blockConsume:   {perMinuteSpec, parallelSpec},
	blockTransform: {parallelSpec},
}

// blockMetaSpec is the full sdk.BlockMeta shape, independent of verb. Every
// field sdk.BlockMeta declares must be present (possibly at its zero
// default) for hclBlock.Decode's strict gocty conversion to succeed, so
// makeBinding evaluates meta values against this rather than metaSpecs —
// blockSchema (built from metaSpecs) is what actually rejects an attribute
// a verb doesn't offer; by the time evalValues runs, an unauthorized
// attribute has already errored and a permitted one is simply absent from
// content.Attributes, so it defaults to zero here either way.
var blockMetaSpec = []*sdk.Spec{perMinuteSpec, stopAfterSpec}
