package config

import (
	"os"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func Load(configBytes []byte) (*Pipelines, error) {
	resourcesRaw := new(ResourcesRaw)
	resourceFile, _ := hclparse.NewParser().ParseHCL(configBytes, "psyduck.psy")
	gohcl.DecodeBody(resourceFile.Body, nil, resourcesRaw)

	resources := makeResources(resourcesRaw)

	pipelinesRaw := new(PipelinesRaw)
	pipelineFile, _ := hclparse.NewParser().ParseHCL(configBytes, "psyduck.psy")
	gohcl.DecodeBody(pipelineFile.Body, nil, pipelinesRaw)

	return makePipelines(pipelinesRaw, resources)
}

func LoadFile(path string) (*Pipelines, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Load(content)
}
