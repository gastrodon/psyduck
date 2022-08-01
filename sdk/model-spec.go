package sdk

import "github.com/zclconf/go-cty/cty"

type Spec struct {
	Name        string
	Description string
	Required    bool
	Type        Type
	Default     cty.Value
}

type SpecMap map[string]*Spec
