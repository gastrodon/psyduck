package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

func Load(configBytes []byte) (PipelineDescriptors, error) {
	configRaw := &ConfigRaw{}
	if err := yaml.Unmarshal(configBytes, configRaw); err != nil {
		panic(err)
	}

	pipelines := make(PipelineDescriptors, len(configRaw.Pipelines))

	for key, pipelineRaw := range configRaw.Pipelines {
		pipeline, err := makePipelineDescriptor(pipelineRaw)
		if err != nil {
			return nil, err
		}

		pipelines[key] = pipeline
	}

	return pipelines, nil
}

func LoadFile(path string) (PipelineDescriptors, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Load(content)
}
