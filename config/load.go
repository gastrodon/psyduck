package config

import (
	"bytes"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func Load(filename string, configBytes []byte) (*Pipelines, error) {
	resourcesRaw := new(ResourcesRaw)
	resourceFile, _ := hclparse.NewParser().ParseHCL(configBytes, filename)
	gohcl.DecodeBody(resourceFile.Body, nil, resourcesRaw)

	resources := makeResources(resourcesRaw)

	pipelinesRaw := new(PipelinesRaw)
	pipelineFile, _ := hclparse.NewParser().ParseHCL(configBytes, filename)
	gohcl.DecodeBody(pipelineFile.Body, nil, pipelinesRaw)

	return makePipelines(pipelinesRaw, resources)
}

func LoadDirectory(configPath string) (*Pipelines, error) {
	configBytes := bytes.NewBuffer(nil)

	paths, err := os.ReadDir(configPath)
	if err != nil {
		return nil, err
	}

	for _, each := range paths {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".psy") {
			continue
		}

		content, err := os.ReadFile(path.Join(configPath, each.Name()))
		if err != nil {
			return nil, err
		}

		configBytes.Write(content)
	}

	return Load(configPath, configBytes.Bytes())
}
