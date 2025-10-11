package core

import (
	"testing"

	"github.com/psyduck-etl/sdk"
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
			Spec:  &sdk.Spec{Type: cty.Number},
		},
		{
			Valid: true,
			Value: cty.StringVal("say hello"),
			Spec:  &sdk.Spec{Type: cty.String},
		},
		{
			Valid: true,
			Value: cty.ListVal([]cty.Value{
				cty.StringVal("huge"),
				cty.StringVal("pixie"),
			}),
			Spec: &sdk.Spec{Type: cty.List(cty.String)},
		},
		{
			Valid: true,
			Value: cty.MapVal(map[string]cty.Value{
				"left":  cty.ListVal([]cty.Value{cty.NumberFloatVal(1.2), cty.NumberFloatVal(0.01)}),
				"right": cty.ListVal([]cty.Value{cty.NumberFloatVal(3.14), cty.NumberFloatVal(2222222.1)}),
			}),
			Spec: &sdk.Spec{Type: cty.Map(cty.List(cty.Number))},
		},
		{
			Valid: true,
			Value: cty.BoolVal(false),
			Spec:  &sdk.Spec{Type: cty.Bool},
		},
		{
			Valid: true,
			Value: cty.MapVal(map[string]cty.Value{
				"honda":   cty.StringVal("civic"),
				"porsche": cty.StringVal("944"),
			}),
			Spec: &sdk.Spec{Type: cty.Map(cty.String)},
		},
		{
			Valid: false,
			Value: cty.NumberIntVal(420_69),
			Spec:  &sdk.Spec{Type: cty.String},
		},
		{
			Valid: false,
			Value: cty.NilVal,
			Spec:  &sdk.Spec{Type: cty.String, Required: true},
		},
	}

	for i, testcase := range cases {
		diags := validate(testcase.Value, testcase.Spec)
		if testcase.Valid != !diags.HasErrors() {
			test.Fatalf("validate[%d]: failed validating: expected valid %v, got %v!", i, testcase.Valid, !diags.HasErrors())
		}
	}
}
