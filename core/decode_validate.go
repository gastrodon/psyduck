package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

func validatePrimitive(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	if !value.Type().Equals(cty.Type(fieldSpec.Type)) {
		return hcl.Diagnostics{
			&hcl.Diagnostic{
				Severity:   hcl.DiagError,
				Expression: hcl.StaticExpr(value, hcl.Range{}),
				Summary:    "invalid primitive type",
				Detail:     fmt.Sprintf("spec requires a %s, got a value %#v", cty.Type(fieldSpec.Type).FriendlyName(), value),
			},
		}
	}

	return nil
}

func validateList(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	elementType := cty.Type(fieldSpec.Type).ListElementType()
	if elementType == nil {
		panic("list element type is nil")
	}

	iter := value.ElementIterator()
	for iter.Next() {
		nextKey, nextValue := iter.Element()
		index, _ := nextKey.AsBigFloat().Int64()
		childSpec := sdk.ListItemSpec(fieldSpec, int(index))
		err := validate(nextValue, childSpec)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateMap(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	elementType := cty.Type(fieldSpec.Type).MapElementType()
	if elementType == nil {
		panic("map element type is nil")
	}

	iter := value.ElementIterator()
	for iter.Next() {
		nextKey, nextValue := iter.Element()
		childSpec := sdk.MapItemSpec(fieldSpec, nextKey.AsString())
		err := validate(nextValue, childSpec)
		if err != nil {
			return err
		}
	}

	return nil
}

func validate(value cty.Value, fieldSpec *sdk.Spec) hcl.Diagnostics {
	if value.Type().IsListType() {
		return validateList(value, fieldSpec)
	}

	if value.Type().IsMapType() {
		return validateMap(value, fieldSpec)
	}

	return validatePrimitive(value, fieldSpec)
}
