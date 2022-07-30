package config

import (
	"github.com/hashicorp/hcl/v2"
)

type Resource struct {
	Kind   string   `hcl:"kind,label" cty:"kind"`
	Name   string   `hcl:"name,label" cty:"name"`
	Remain hcl.Body `hcl:",remain"`
}

type ResourcesRaw struct {
	Producers    []*Resource `hcl:"produce,block" cty:"produce"`
	Consumers    []*Resource `hcl:"consume,block" cty:"consume"`
	Transformers []*Resource `hcl:"transform,block" cty:"transform"`
}

type Resources struct {
	Producers    map[string]*Resource
	Consumers    map[string]*Resource
	Transformers map[string]*Resource
}
