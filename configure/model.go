package configure

import (
	"github.com/hashicorp/hcl/v2"
)

/*
{produce,consume,transform} "kind" "name" {
	foo = "bar"
	ref = value.ref
}
*/

type pipelinePart struct {
	Kind    string   `hcl:"kind,label" cty:"kind"`
	Name    string   `hcl:"name,label" cty:"kind"`
	Options hcl.Body `hcl:",remain"`
}

type pipelineParts struct {
	Producers    []*pipelinePart `hcl:"produce,block"`
	Consumers    []*pipelinePart `hcl:"consume,block"`
	Transformers []*pipelinePart `hcl:"transform,block"`
}

type pipelineBlock struct {
	RemoteProducer    *string  `cty:"produce-from"`
	Producers         []string `cty:"produce"`
	Consumers         []string `cty:"consume"`
	Transformers      []string `cty:"transform"`
	StopAfter         *int     `cty:"stop-after"`
	ExitOnError       *bool    `cty:"exit-on-error"`
	ParallelProducers *uint    `cty:"parallel-producers"`
}

type Pipeline struct {
	Name              string
	RemoteProducer    *pipelinePart
	Producers         []*pipelinePart
	Consumers         []*pipelinePart
	Transformers      []*pipelinePart
	StopAfter         int
	ExitOnError       bool
	ParallelProducers uint
}
