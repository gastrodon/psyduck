package config

import (
	"fmt"

	"github.com/gastrodon/psyduck/model"
)

func makePipelineDescriptor(data model.ETLPipelineRaw) (*model.PipelineDescriptor, error) {
	producer, err := makeDescriptor(data.Producer)
	if err != nil {
		return nil, err
	}

	consumer, err := makeDescriptor(data.Consumer)
	if err != nil {
		return nil, err
	}

	transformers := make([]model.Descriptor, len(data.Transformers))
	for index, descriptor := range data.Transformers {
		transformer, err := makeDescriptor(descriptor)
		if err != nil {
			return nil, err
		}

		transformers[index] = *transformer
	}

	return &model.PipelineDescriptor{
		Producer:     *producer,
		Consumer:     *consumer,
		Transformers: transformers,
	}, nil
}

func makeDescriptor(data map[string]interface{}) (*model.Descriptor, error) {
	kind, ok := data["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("data does not have a kind %#v", data)
	}

	return &model.Descriptor{
		Kind:   kind,
		Config: data,
	}, nil
}
