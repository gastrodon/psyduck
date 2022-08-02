package config

import (
	"bytes"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func loadValuesContext(filename string, configBytes []byte) (*hcl.EvalContext, error) {
	values := new(Values)
	file, diags := hclparse.NewParser().ParseHCL(configBytes, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, nil, values)
	return makeValuesContext(values), nil
}

func loadResources(filename string, configBytes []byte, context *hcl.EvalContext) (*Resources, error) {
	raw := new(ResourcesRaw)
	file, diags := hclparse.NewParser().ParseHCL(configBytes, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, context, raw)
	return makeResources(raw), nil
}

func loadPipelines(filename string, configBytes []byte, resources *Resources) (*Pipelines, error) {
	raw := new(PipelinesRaw)
	file, diags := hclparse.NewParser().ParseHCL(configBytes, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, nil, raw)
	return makePipelines(raw, resources)
}

func Load(filename string, configBytes []byte) (*Pipelines, error) {
	valuesContext, err := loadValuesContext(filename, configBytes)
	if err != nil {
		return nil, err
	}

	resources, err := loadResources(filename, configBytes, valuesContext)
	if err != nil {
		return nil, err
	}

	// TODO in the future we want a resourcesContext here, I think
	return loadPipelines(filename, configBytes, resources)
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
