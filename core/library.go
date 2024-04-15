package core

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib"
)

type Library struct {
	Producer    func(string, *hcl.EvalContext, hcl.Body) (sdk.Producer, error)
	Consumer    func(string, *hcl.EvalContext, hcl.Body) (sdk.Consumer, error)
	Transformer func(string, *hcl.EvalContext, hcl.Body) (sdk.Transformer, error)
}

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

func makeParser(providedSpecMap sdk.SpecMap, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Parser, sdk.SpecParser) {
	parser := func(spec sdk.SpecMap, target interface{}) error {
		content, _, diags := config.PartialContent(makeBodySchema(spec))
		if diags.HasErrors() {
			return diags
		}

		if diags := decodeAttributes(spec, evalCtx, content.Attributes, target); diags.HasErrors() {
			return diags
		}

		return nil
	}

	return func(target interface{}) error {
		return parser(providedSpecMap, target)
	}, parser
}

func NewLibrary(plugins []*sdk.Plugin) *Library {
	lookupResource := make(map[string]*sdk.Resource)
	for _, plugin := range append(plugins, stdlib.Plugin()) {
		for _, resource := range plugin.Resources {
			lookupResource[resource.Name] = resource
		}
	}

	return &Library{
		Producer: func(name string, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Producer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.PRODUCER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
			}

			return found.ProvideProducer(makeParser(found.Spec, evalCtx, config))
		},
		Consumer: func(name string, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Consumer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.CONSUMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideConsumer(makeParser(found.Spec, evalCtx, config))
		},
		Transformer: func(name string, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Transformer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.TRANSFORMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideTransformer(makeParser(found.Spec, evalCtx, config))
		},
	}
}
