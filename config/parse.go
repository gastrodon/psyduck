package config

import (
	"fmt"

	"github.com/gastrodon/psyduck/model"
	"gopkg.in/yaml.v3"
)

func Load(configBytes []byte) (*model.ETLConfig, error) {
	configRaw := model.ETLConfigRaw{}
	if err := yaml.Unmarshal(configBytes, &configRaw); err != nil {
		panic(err)
	}

	pipelines := make(map[string]model.PipelineDescriptor, len(configRaw.Pipelines))
	for key, pipelineRaw := range configRaw.Pipelines {
		pipeline, err := makePipelineDescriptor(pipelineRaw)
		if err != nil {
			return nil, err
		}

		pipelines[key] = *pipeline
	}

	return &model.ETLConfig{
		Pipelines: pipelines,
	}, nil
}

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
		return nil, fmt.Errorf("Data does not have a kind! %#v\n", data)
	}

	return &model.Descriptor{
		Kind:   kind,
		Config: data,
	}, nil
}
