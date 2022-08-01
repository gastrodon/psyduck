package core

import (
	"fmt"
	"reflect"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

const TAG = "psy"

func makeTagMap(source interface{}) map[string]reflect.Value {
	value := reflect.ValueOf(source)

	fields := reflect.VisibleFields(value.Elem().Type())
	tagMap := make(map[string]reflect.Value, len(fields))
	for _, field := range fields {
		if tag, ok := field.Tag.Lookup(TAG); ok {
			tagMap[tag] = value.Elem().FieldByIndex(field.Index)
		}
	}

	return tagMap
}

func getFieldValue(fieldSpec *sdk.Spec, attr *hcl.Attribute) (cty.Value, error) {
	attrValue, err := attr.Expr.Value(nil)
	if err != nil {
		return cty.NilVal, err
	}

	if attrValue.IsNull() {
		if fieldSpec.Required {
			return cty.NilVal, fmt.Errorf("%s requires a value", fieldSpec.Name)
		}

		return fieldSpec.Default, nil
	}

	if !attrValue.Type().Equals(cty.Type(fieldSpec.Type)) {
		return cty.NilVal, fmt.Errorf("%s requires a %v", fieldSpec.Name, fieldSpec.Type)
	}

	return attrValue, nil
}

func setField(target reflect.Value, value cty.Value, fieldSpec *sdk.Spec) error {
	if !target.CanAddr() {
		return fmt.Errorf("can't address the field tagged %s", fieldSpec.Name)
	}

	if !target.CanSet() {
		return fmt.Errorf("can't set the field tagged %s", fieldSpec.Name)
	}

	switch fieldSpec.Type {
	case sdk.Bool:
		target.SetBool(value.True())
	case sdk.String:
		target.SetString(value.AsString())
	case sdk.Integer:
		if !value.AsBigFloat().IsInt() {
			return fmt.Errorf("%s requires an integer", fieldSpec.Name)
		}

		result, _ := value.AsBigFloat().Int64()
		target.SetInt(result)
	case sdk.Float:
		if !value.AsBigFloat().IsInt() {
			return fmt.Errorf("%s requires a float", fieldSpec.Name)
		}

		result, _ := value.AsBigFloat().Float64()
		target.SetFloat(result)
	default:
		return fmt.Errorf("%v is unimplemented", fieldSpec.Type)
	}

	return nil
}

func decodeConfig(body hcl.Body, spec sdk.SpecMap, target interface{}) error {
	content, _, diags := body.PartialContent(makeBodySchema(spec))
	if diags != nil {
		return diags
	}

	for name, target := range makeTagMap(target) {
		fieldSpec, ok := spec[name]
		if !ok {
			return fmt.Errorf("missing spec for %s", name)
		}

		if attr, ok := content.Attributes[name]; ok {
			fieldValue, err := getFieldValue(fieldSpec, attr)
			if err != nil {
				return err
			}

			if err := setField(target, fieldValue, fieldSpec); err != nil {
				return err
			}

			continue
		}

		if fieldSpec.Required {
			return fmt.Errorf("missing required value %s", name)
		}

		if err := setField(target, fieldSpec.Default, fieldSpec); err != nil {
			return err
		}
	}

	return nil
}
