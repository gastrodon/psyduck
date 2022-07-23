package config

import (
	"os"

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

func LoadFile(path string) (*model.ETLConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Load(content)
}
