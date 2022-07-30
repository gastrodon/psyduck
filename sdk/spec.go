package sdk

import (
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

func SpecPerMinute(value int64) hcldec.Spec {
	return &hcldec.DefaultSpec{
		Primary: &hcldec.AttrSpec{
			Name:     "per-minute",
			Type:     cty.Number,
			Required: false,
		},
		Default: &hcldec.LiteralSpec{
			Value: cty.NumberIntVal(value),
		},
	}
}

func SpecExitOnError(value bool) hcldec.Spec {
	return &hcldec.DefaultSpec{
		Primary: &hcldec.AttrSpec{
			Name:     "exit-on-error",
			Type:     cty.Bool,
			Required: false,
		},
		Default: &hcldec.LiteralSpec{
			Value: cty.BoolVal(value),
		},
	}
}
