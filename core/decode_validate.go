package core

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

func itemSpec(source *sdk.Spec, key string, baseType *cty.Type) *sdk.Spec {
	name := strings.Join([]string{source.Name, key}, ".")
	if baseType == nil {
		panic(fmt.Sprintf("cannot gather element type of %s", name))
	}

	return &sdk.Spec{
		Name:        name,
		Description: source.Description,
		Required:    source.Required,
		Type:        *baseType,
		Default:     cty.NilVal,
	}
}

func listItemSpec(source *sdk.Spec, index int64) *sdk.Spec {
	return itemSpec(source, strconv.FormatInt(index, 10), cty.Type(source.Type).ListElementType())
}

func mapItemSpec(source *sdk.Spec, key string) *sdk.Spec {
	return itemSpec(source, key, cty.Type(source.Type).MapElementType())
}

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
		childSpec := listItemSpec(fieldSpec, index)
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
		childSpec := mapItemSpec(fieldSpec, nextKey.AsString())
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
