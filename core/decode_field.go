package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

func checkType(value cty.Value, fieldSpec *sdk.Spec) error {
	if !value.Type().Equals(cty.Type(fieldSpec.Type)) {
		return fmt.Errorf("%s requires a %#v, but has a %#v", fieldSpec.Name, fieldSpec.Type, value.Type())
	}

	return nil
}

func valuePrimitive(value cty.Value, fieldSpec *sdk.Spec) (cty.Value, error) {
	return value, checkType(value, fieldSpec)
}

func valueListy(value cty.Value, fieldSpec *sdk.Spec) (cty.Value, error) {
	elementType := cty.Type(fieldSpec.Type).ListElementType()
	if elementType == nil {
		panic("list element type is nil")
	}

	index := 0
	childs := make([]cty.Value, value.LengthInt())
	iter := value.ElementIterator()
	for iter.Next() {
		_, nextValue := iter.Element()
		childSpec := sdk.ListItemSpec(fieldSpec, index)
		nextValueDecode, err := valueFromSpec(nextValue, childSpec)
		if err != nil {
			return cty.NilVal, err
		}

		childs[index] = nextValueDecode
		index++
	}

	return cty.ListVal(childs), nil
}

func valueMappy(value cty.Value, fieldSpec *sdk.Spec) (cty.Value, error) {
	elementType := cty.Type(fieldSpec.Type).MapElementType()
	if elementType == nil {
		panic("map element type is nil")
	}

	childs := make(map[string]cty.Value, value.LengthInt())
	iter := value.ElementIterator()
	for iter.Next() {
		nextKey, nextValue := iter.Element()
		nextKeyDecode := nextKey.AsString()
		childSpec := sdk.MapItemSpec(fieldSpec, nextKeyDecode)
		nextValueDecode, err := valueFromSpec(nextValue, childSpec)
		if err != nil {
			return cty.NilVal, err
		}

		childs[nextKeyDecode] = nextValueDecode
	}

	return cty.MapVal(childs), nil
}

func valueFromSpec(value cty.Value, fieldSpec *sdk.Spec) (cty.Value, error) {
	if value.Type().IsListType() {
		return valueListy(value, fieldSpec)
	}

	if value.Type().IsMapType() {
		return valueMappy(value, fieldSpec)
	}

	return valuePrimitive(value, fieldSpec)
}

func decodeAttribute(attr *hcl.Attribute, fieldSpec *sdk.Spec, context *hcl.EvalContext) (cty.Value, error) {
	attrValue, err := attr.Expr.Value(context)
	if err != nil {
		return cty.NilVal, err
	}

	if attrValue.IsNull() {
		if fieldSpec.Required {
			return cty.NilVal, fmt.Errorf("%s requires a value", fieldSpec.Name)
		}

		return fieldSpec.Default, nil
	}

	return valueFromSpec(attrValue, fieldSpec)
}
