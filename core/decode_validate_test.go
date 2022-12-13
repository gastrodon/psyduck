package core

import (
	"testing"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func TestValidate(test *testing.T) {
	cases := []struct {
		Valid bool
		Value cty.Value
		Spec  *sdk.Spec
	}{
		{
			Valid: true,
			Value: cty.NumberIntVal(420_69),
			Spec:  &sdk.Spec{Type: sdk.Integer},
		},
		{
			Valid: true,
			Value: cty.StringVal("say hello"),
			Spec:  &sdk.Spec{Type: sdk.String},
		},
		{
			Valid: true,
			Value: cty.ListVal([]cty.Value{
				cty.StringVal("huge"),
				cty.StringVal("pixie"),
			}),
			Spec: &sdk.Spec{Type: sdk.List(sdk.String)},
		},
		{
			Valid: true,
			Value: cty.MapVal(map[string]cty.Value{
				"left":  cty.ListVal([]cty.Value{cty.NumberFloatVal(1.2), cty.NumberFloatVal(0.01)}),
				"right": cty.ListVal([]cty.Value{cty.NumberFloatVal(3.14), cty.NumberFloatVal(2222222.1)}),
			}),
			Spec: &sdk.Spec{Type: sdk.Map(sdk.List(sdk.Float))},
		},
		{
			Valid: true,
			Value: cty.BoolVal(false),
			Spec:  &sdk.Spec{Type: sdk.Bool},
		},
		{
			Valid: true,
			Value: cty.MapVal(map[string]cty.Value{
				"honda":   cty.StringVal("civic"),
				"porsche": cty.StringVal("944"),
			}),
			Spec: &sdk.Spec{Type: sdk.Map(sdk.String)},
		},
		{
			Valid: false,
			Value: cty.NumberIntVal(420_69),
			Spec:  &sdk.Spec{Type: sdk.String},
		},
		{
			Valid: false,
			Value: cty.NilVal,
			Spec:  &sdk.Spec{Type: sdk.String, Required: true},
		},
	}

	for _, testcase := range cases {
		diags := validate(testcase.Value, testcase.Spec)
		assert.Equal(test, testcase.Valid, !diags.HasErrors(), "%s", diags)
	}
}
