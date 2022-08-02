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

func loadValues(filename string, configBytes []byte) (*Values, error) {
	values := new(Values)
	file, diags := hclparse.NewParser().ParseHCL(configBytes, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, nil, values)
	return values, nil
}

func loadResources(filename string, configBytes []byte) (*Resources, error) {
	raw := new(ResourcesRaw)
	file, diags := hclparse.NewParser().ParseHCL(configBytes, filename)
	if diags != nil {
		return nil, diags
	}

	gohcl.DecodeBody(file.Body, nil, raw)
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

func Load(filename string, configBytes []byte) (*Pipelines, *hcl.EvalContext, error) {
	values, err := loadValues(filename, configBytes)
	if err != nil {
		return nil, nil, err
	}

	resources, err := loadResources(filename, configBytes)
	if err != nil {
		return nil, nil, err
	}

	pipelines, err := loadPipelines(filename, configBytes, resources)
	if err != nil {
		return nil, nil, err
	}

	context, err := makeContext(values)
	if err != nil {
		return nil, nil, err
	}
	return pipelines, context, nil
}

func LoadDirectory(configPath string) (*Pipelines, *hcl.EvalContext, error) {
	configBytes := bytes.NewBuffer(nil)

	paths, err := os.ReadDir(configPath)
	if err != nil {
		return nil, nil, err
	}

	for _, each := range paths {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".psy") {
			continue
		}

		content, err := os.ReadFile(path.Join(configPath, each.Name()))
		if err != nil {
			return nil, nil, err
		}

		configBytes.Write(content)
	}

	return Load(configPath, configBytes.Bytes())
}
