package configure

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func Partial(filename string, literal []byte, context *hcl.EvalContext) (*pipelineParts, error) {
	file, diags := hclparse.NewParser().ParseHCL(literal, filename)
	if diags != nil {
		return nil, diags
	}

	resources := new(pipelineParts)
	if diags := gohcl.DecodeBody(file.Body, context, resources); !diags.HasErrors() {
		return nil, diags
	}

	return resources, nil
}

func Literal(filename string, literal []byte) (map[string]*Pipeline, *hcl.EvalContext, error) {
	valuesContext, err := loadValuesContext(filename, literal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load values ctx: %s", err)
	}

	resourcesContext, err := loadResourcesContext(filename, literal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load resources ctx: %s", err)
	}

	resourceLookup, err := loadResorceLookup(filename, literal, valuesContext)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load resources lookup: %s", err)
	}

	pipelines, err := loadPipelines(filename, literal, resourcesContext, resourceLookup)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pipelines: %s", err)
	}

	return pipelines, valuesContext, nil
}

func ReadDirectory(directory string) ([]byte, error) {
	literal := bytes.NewBuffer(nil)
	paths, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to read files in %s: %s", directory, err)
	}

	for _, each := range paths {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".psy") {
			continue
		}

		if content, err := os.ReadFile(path.Join(directory, each.Name())); err != nil {
			return nil, fmt.Errorf("failed reading %s: %s", each.Name(), err)
		} else {
			literal.Write(content)
		}
	}

	return literal.Bytes(), nil
}
