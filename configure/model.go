package configure

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

type Values struct {
	Blocks []struct {
		Entries map[string]cty.Value `hcl:",remain"`
	} `hcl:"value,block"`
}

type ResourceHeader struct {
	Kind string `cty:"kind"`
	Name string `cty:"kind"`
}

type Resource struct {
	Kind    string   `hcl:"kind,label" cty:"kind"`
	Name    string   `hcl:"name,label" cty:"kind"`
	Options hcl.Body `hcl:",remain"`
}

type Resources struct {
	Producers    []*Resource `hcl:"produce,block"`
	Consumers    []*Resource `hcl:"consume,block"`
	Transformers []*Resource `hcl:"transform,block"`
}

type PipelineRef struct {
	Producers    []string `cty:"produce"`
	Consumers    []string `cty:"consume"`
	Transformers []string `cty:"transform"`
}

type Pipeline struct {
	Name         string      `hcl:"name,label"`
	Producers    []*Resource `hcl:"produce"`
	Consumers    []*Resource `hcl:"consume"`
	Transformers []*Resource `hcl:"transform"`
}

type Pipelines struct {
	Pipelines []*Pipeline `hcl:"pipeline,block"`
}
