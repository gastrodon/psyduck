package core

import (
	"testing"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

var cases = []struct {
	Attr    hcl.Attribute
	Spec    *sdk.Spec
	Context *hcl.EvalContext
	Want    cty.Value
}{
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.NumberIntVal(420_69), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.Integer},
		Want: cty.NumberIntVal(420_69),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.StringVal("say hello"), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.String},
		Want: cty.StringVal("say hello"),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.ListVal([]cty.Value{
			cty.StringVal("huge"),
			cty.StringVal("pixie"),
		}), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.List(sdk.String)},
		Want: cty.ListVal([]cty.Value{
			cty.StringVal("huge"),
			cty.StringVal("pixie"),
		}),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.MapVal(map[string]cty.Value{
			"left":  cty.ListVal([]cty.Value{cty.NumberFloatVal(1.2), cty.NumberFloatVal(0.01)}),
			"right": cty.ListVal([]cty.Value{cty.NumberFloatVal(3.14), cty.NumberFloatVal(2222222.1)}),
		}), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.Map(sdk.List(sdk.Float))},
		Want: cty.MapVal(map[string]cty.Value{
			"left":  cty.ListVal([]cty.Value{cty.NumberFloatVal(1.2), cty.NumberFloatVal(0.01)}),
			"right": cty.ListVal([]cty.Value{cty.NumberFloatVal(3.14), cty.NumberFloatVal(2222222.1)}),
		}),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.NilVal, hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.Bool, Default: cty.BoolVal(true)},
		Want: cty.BoolVal(true),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.BoolVal(false), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.Bool, Default: cty.BoolVal(true)},
		Want: cty.BoolVal(false),
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.MapVal(map[string]cty.Value{
			"honda":   cty.StringVal("civic"),
			"porsche": cty.StringVal("944"),
		}), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.Map(sdk.String)},
		Want: cty.MapVal(map[string]cty.Value{
			"honda":   cty.StringVal("civic"),
			"porsche": cty.StringVal("944"),
		}),
	},
}

func TestDecodeValue(test *testing.T) {
	for _, testcase := range cases {
		value, err := decodeAttribute(&testcase.Attr, testcase.Spec, testcase.Context)
		assert.Nil(test, err, err)
		assert.True(test, value.Equals(testcase.Want).True(),
			"value mismatch! have %s, want %s", value.GoString(), testcase.Want.GoString())
	}
}
