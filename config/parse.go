package config

import (
	"fmt"
)

func makePipelineDescriptor(data PipelineRaw) (*PipelineDescriptor, error) {
	producers, err := makeDescriptors(data.Producers)
	if err != nil {
		return nil, err
	}

	consumers, err := makeDescriptors(data.Consumers)
	if err != nil {
		return nil, err
	}

	transformers, err := makeDescriptors(data.Transformers)
	if err != nil {
		return nil, err
	}

	return &PipelineDescriptor{
		Producers:    producers,
		Consumers:    consumers,
		Transformers: transformers,
	}, nil
}

func makeDescriptors(data []map[string]interface{}) ([]*Descriptor, error) {
	descriptors := make([]*Descriptor, len(data))
	for index, rawDescriptor := range data {
		descriptor, err := makeDescriptor(rawDescriptor)
		if err != nil {
			return nil, err
		}

		descriptors[index] = descriptor
	}

	return descriptors, nil
}

func makeDescriptor(data map[string]interface{}) (*Descriptor, error) {
	kind, ok := data["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("data does not have a kind %#v", data)
	}

	return &Descriptor{
		Kind:   kind,
		Config: data,
	}, nil
}
