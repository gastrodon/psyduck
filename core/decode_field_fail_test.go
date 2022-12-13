package core

import (
	"testing"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

var decodeCasesFail = []struct {
	Attr    hcl.Attribute
	Spec    *sdk.Spec
	Context *hcl.EvalContext
}{
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.NumberIntVal(420_69), hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.String},
	},
	{
		Attr: hcl.Attribute{Expr: hcl.StaticExpr(cty.NilVal, hcl.Range{})},
		Spec: &sdk.Spec{Type: sdk.String, Required: true},
	},
}

func TestDecodeValueFail(test *testing.T) {
	for _, testcase := range decodeCasesFail {
		value, err := decodeAttribute(&testcase.Attr, testcase.Spec, testcase.Context)
		assert.Error(test, err, "value should have failed to parse, got %s", value.GoString())
	}
}
