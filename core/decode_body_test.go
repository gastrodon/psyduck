package core

import (
	"reflect"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

func TestDecodeConfig(test *testing.T) {
	spec := sdk.SpecMap{
		"test":     {Name: "test", Type: cty.String},
		"count":    {Name: "count", Type: cty.Number},
		"values":   {Name: "values", Type: cty.List(cty.Number)},
		"map":      {Name: "map", Type: cty.Map(cty.Bool)},
		"default":  {Name: "default", Type: cty.String, Default: cty.StringVal("default")},
		"override": {Name: "override", Type: cty.String, Default: cty.StringVal("default")},
	}

	attrs := hcl.Attributes{
		"test": {
			Name: "test",
			Expr: hcl.StaticExpr(cty.StringVal("passed"), hcl.Range{}),
		},
		"count": {
			Name: "count",
			Expr: hcl.StaticExpr(cty.NumberIntVal(100), hcl.Range{}),
		},
		"values": {
			Name: "values",
			Expr: hcl.StaticExpr(cty.ListVal([]cty.Value{
				cty.NumberIntVal(10), cty.NumberIntVal(20),
			}), hcl.Range{}),
		},
		"map": {
			Name: "map",
			Expr: hcl.StaticExpr(cty.MapVal(map[string]cty.Value{
				"left":  cty.BoolVal(false),
				"right": cty.BoolVal(true),
			}), hcl.Range{}),
		},
		"override": {
			Name: "override",
			Expr: hcl.StaticExpr(cty.StringVal("overridden"), hcl.Range{}),
		},
	}

	type dummy struct {
		Test     string          `psy:"test"`
		Count    int             `psy:"count"`
		Values   []int           `psy:"values"`
		Map      map[string]bool `psy:"map"`
		Default  string          `psy:"default"`
		Override string          `psy:"override"`
	}

	target := new(dummy)
	want := dummy{
		Test:     "passed",
		Count:    100,
		Values:   []int{10, 20},
		Map:      map[string]bool{"left": false, "right": true},
		Default:  "default",
		Override: "overridden",
	}

	diags := decodeAttributes(spec, nil, attrs, target)
	if diags.HasErrors() {
		test.Errorf("unexpected errors: %s", diags)
	}
	if !reflect.DeepEqual(want, *target) {
		test.Errorf("expected %#v, got %#v", want, *target)
	}
}
