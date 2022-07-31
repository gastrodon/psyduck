package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
)

func makeParser(config hcl.Body, providedSpecMap sdk.SpecMap) (sdk.Parser, sdk.SpecParser) {
	parser := func(specMap sdk.SpecMap, target interface{}) error {
		decoded, diags := hcldec.Decode(config, buildSpecMap(specMap), nil) // TODO ctx with variables goes here!
		if diags != nil {
			return diags
		}

		panic(fmt.Errorf("%#v", decoded)) // cty.Value
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
		ProvideProducer: func(name string, config hcl.Body) (sdk.Producer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.PRODUCER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
			}

			return found.ProvideProducer(makeParser(config, found.Spec))
		},
		ProvideConsumer: func(name string, config hcl.Body) (sdk.Consumer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.CONSUMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideConsumer(makeParser(config, found.Spec))
		},
		ProvideTransformer: func(name string, config hcl.Body) (sdk.Transformer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.TRANSFORMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideTransformer(makeParser(config, found.Spec))
		},
	}
}
