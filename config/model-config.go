package config

import "github.com/hashicorp/hcl/v2"

type ConfigRaw struct {
	Producers []struct {
		Kind   string   `hcl:"kind,label"`
		Name   string   `hcl:"name,label"`
		Remain hcl.Body `hcl:",remain"`
	} `hcl:"produce,block"`
	Consumers []struct {
		Kind   string   `hcl:"kind,label"`
		Name   string   `hcl:"name,label"`
		Remain hcl.Body `hcl:",remain"`
	} `hcl:"consume,block"`
	Transformers []struct {
		Kind   string   `hcl:"kind,label"`
		Name   string   `hcl:"name,label"`
		Remain hcl.Body `hcl:",remain"`
	} `hcl:"transform,block"`
	Pipelines []struct {
		Name           string   `hcl:"name,label"`
		ProducerRef    []string `hcl:"producers,label"`
		ConsumerRef    []string `hcl:"consumers,label"`
		TransformerRef []string `hcl:"transformers,label"`
	} `hcl:"pipeline,block"`
}
