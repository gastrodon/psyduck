package config

import "github.com/zclconf/go-cty/cty"

type Values struct {
	Values map[string]cty.Value `hcl:"values,block"`
}
