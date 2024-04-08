package core

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

func toListVal(value cty.Value) cty.Value {
	items := make([]cty.Value, value.LengthInt())
	iter := value.ElementIterator()
	for iter.Next() {
		nextIndex, nextValue := iter.Element()
		index, _ := nextIndex.AsBigFloat().Int64()
		items[int(index)] = nextValue
	}

	return cty.ListVal(items)
}

func getAttributeValue(attributes hcl.Attributes, name string, context *hcl.EvalContext) (cty.Value, hcl.Diagnostics) {
	if attr, ok := attributes[name]; ok {
		return attr.Expr.Value(context)
	} else {
		return cty.NilVal, nil
	}
}

func decodeAttributes(spec sdk.SpecMap, context *hcl.EvalContext, attributes hcl.Attributes, target interface{}) hcl.Diagnostics {
	diags := hcl.Diagnostics{}

	valuesDecode := make(map[string]cty.Value)
	for name, fieldSpec := range spec {
		fieldValue, diagsValue := getAttributeValue(attributes, name, context)
		if diagsValue.HasErrors() {
			for _, each := range diagsValue {
				diags = diags.Append(each)
			}

			continue
		}

		if fieldValue.IsNull() {
			if fieldSpec.Required {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "value required",
					Detail:   fmt.Sprintf("a value is required for %s", fieldSpec.Name),
				})
			} else {
				valuesDecode[name] = fieldSpec.Default
			}
			continue
		}

		if fieldValue.Type().IsTupleType() {
			fieldValue = toListVal(fieldValue)
		}

		if diagsValidate := validate(fieldValue, fieldSpec); diagsValidate.HasErrors() {
			for _, each := range diagsValidate {
				diags = diags.Append(each)
			}

			continue
		}

		valuesDecode[name] = fieldValue
	}

	if err := gocty.FromCtyValueTagged(cty.ObjectVal(valuesDecode), target, "psy"); err != nil {
		panic(err)
	}

	return diags
}
