package config

import (
	"fmt"
)

func makePipelines(raw *PipelinesRaw, resources *Resources) (*Pipelines, error) {
	pipelines := make(map[string]*Pipeline, len(raw.Pipelines))

	for _, pipelineRaw := range raw.Pipelines {
		pipeline, err := makePipeline(pipelineRaw, resources)
		if err != nil {
			return nil, err
		}

		pipelines[pipeline.Name] = pipeline
	}

	return &Pipelines{Pipelines: pipelines}, nil
}

func makePipeline(raw *PipelineRaw, resources *Resources) (*Pipeline, error) {
	producers, err := refLookup(raw.ProducerRef, resources.Producers)
	if err != nil {
		return nil, err
	}

	consumers, err := refLookup(raw.ConsumerRef, resources.Consumers)
	if err != nil {
		return nil, err
	}

	transformers, err := refLookup(raw.TransformerRef, resources.Transformers)
	if err != nil {
		return nil, err
	}

	return &Pipeline{
		Name:         raw.Name,
		Producers:    producers,
		Consumers:    consumers,
		Transformers: transformers,
	}, nil
}

func refLookup(ref []string, lookup map[string]*Resource) ([]*Resource, error) {
	bodies := make([]*Resource, len(ref))
	for index, name := range ref {
		body, ok := lookup[name]
		if !ok {
			return nil, fmt.Errorf("can't find reference %s", name)
		}

		bodies[index] = body
	}

	return bodies, nil
}
