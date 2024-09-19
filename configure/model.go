package configure

/*
{produce,consume,transform} "kind" "name" {
	foo = "bar"
	ref = value.ref
}
*/

type pipelineParts struct {
	Producers    []*MoverDesc `hcl:"produce,block"`
	Consumers    []*MoverDesc `hcl:"consume,block"`
	Transformers []*MoverDesc `hcl:"transform,block"`
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
