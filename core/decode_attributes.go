package core

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

func toDynCollection(value cty.Value) cty.Value {
	switch {
	case value.Type().IsTupleType():
		items := make([]cty.Value, value.LengthInt())
		iter := value.ElementIterator()
		for iter.Next() {
			nextIndex, nextValue := iter.Element()
			index, _ := nextIndex.AsBigFloat().Int64()
			items[int(index)] = toDynCollection(nextValue)
		}

		return cty.ListVal(items)
	case value.Type().IsObjectType():
		items := make(map[string]cty.Value)
		iter := value.ElementIterator()
		for iter.Next() {
			nextKey, nextValue := iter.Element()
			key := nextKey.AsString()
			items[key] = toDynCollection(nextValue)
		}

		return cty.MapVal(items)
	default:
		return value
	}
}

func getAttributeValue(attributes hcl.Attributes, name string, evalCtx *hcl.EvalContext) (cty.Value, hcl.Diagnostics) {
	if attr, ok := attributes[name]; ok {
		return attr.Expr.Value(evalCtx)
	} else {
		return cty.NilVal, nil
	}
}

func decodeAttributes(spec sdk.SpecMap, evalCtx *hcl.EvalContext, attributes hcl.Attributes, target interface{}) hcl.Diagnostics {
	diags := hcl.Diagnostics{}

	valuesDecode := make(map[string]cty.Value)
	for name, fieldSpec := range spec {
		fieldValue, diagsValue := getAttributeValue(attributes, name, evalCtx)
		if diagsValue.HasErrors() {
			for _, each := range diagsValue {
				diags = diags.Append(each)
			}

			continue
		}

		if fieldValue.IsNull() {
			if fieldSpec.Required {
				diags = diags.Append(&hcl.Diagnostic{
					Severity:    hcl.DiagError,
					Summary:     "value required",
					Detail:      fmt.Sprintf("a value is required for %s", fieldSpec.Name),
					EvalContext: evalCtx,
				})
			} else {
				valuesDecode[name] = fieldSpec.Default
			}
			continue
		}

		fieldValue = toDynCollection(fieldValue)
		if diagsValidate := validate(fieldValue, fieldSpec); diagsValidate.HasErrors() {
			for _, each := range diagsValidate {
				diags = diags.Append(each)
			}

			continue
		}

		valuesDecode[name] = fieldValue
	}

	if err := gocty.FromCtyValueTagged(cty.ObjectVal(valuesDecode), target, "psy"); err != nil {
		diags.Append(&hcl.Diagnostic{
			Severity:    hcl.DiagError,
			Summary:     "fromCtyValueTagged failed",
			Detail:      fmt.Sprintf("failed to decode cty value: %s", err),
			EvalContext: evalCtx,
		})
	}

	return diags
}
