package core

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-std/sdk"
)

func makeBodySchema(specMap sdk.SpecMap) *hcl.BodySchema {
	attributes := make([]hcl.AttributeSchema, len(specMap))

	index := 0
	for _, spec := range specMap {
		attributes[index] = hcl.AttributeSchema{
			Name:     spec.Name,
			Required: spec.Required,
		}

		index++
	}

	return &hcl.BodySchema{
		Attributes: attributes,
	}
}

func makeParser(providedSpecMap sdk.SpecMap, context *hcl.EvalContext, config hcl.Body) (sdk.Parser, sdk.SpecParser) {
	parser := func(spec sdk.SpecMap, target interface{}) error {
		content, _, diags := config.PartialContent(makeBodySchema(spec))
		if diags.HasErrors() {
			return diags
		}

		if diags := decodeAttributes(spec, context, content.Attributes, target); diags.HasErrors() {
			return diags
		}

		return nil
	}

	return func(target interface{}) error {
		return parser(providedSpecMap, target)
	}, parser
}

func NewLibrary() *Library {
	lookupResource := make(map[string]*sdk.Resource)

	return &Library{
		Load: func(plugin *sdk.Plugin) {
			for _, resource := range plugin.Resources {
				lookupResource[resource.Name] = resource
			}
		},
		ProvideProducer: func(name string, context *hcl.EvalContext, config hcl.Body) (sdk.Producer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.PRODUCER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
			}

			return found.ProvideProducer(makeParser(found.Spec, context, config))
		},
		ProvideConsumer: func(name string, context *hcl.EvalContext, config hcl.Body) (sdk.Consumer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.CONSUMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideConsumer(makeParser(found.Spec, context, config))
		},
		ProvideTransformer: func(name string, context *hcl.EvalContext, config hcl.Body) (sdk.Transformer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.TRANSFORMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideTransformer(makeParser(found.Spec, context, config))
		},
	}
}
