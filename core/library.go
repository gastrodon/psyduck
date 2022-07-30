package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty/gocty"
)

func makeParser(config hcl.Body, spec *hcldec.ObjectSpec) func(interface{}) error {
	return func(target interface{}) error {
		decoded, diags := hcldec.Decode(config, spec, nil)
		if diags != nil {
			return diags
		}

		return gocty.FromCtyValue(decoded, target)
	}
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

			return found.ProvideProducer(makeParser(config, &found.Spec))
		},
		ProvideConsumer: func(name string, config hcl.Body) (sdk.Consumer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.CONSUMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideConsumer(makeParser(config, &found.Spec))
		},
		ProvideTransformer: func(name string, config hcl.Body) (sdk.Transformer, error) {
			found, ok := lookupResource[name]
			if !ok {
				return nil, fmt.Errorf("can't find resource %s", name)
			}

			if found.Kinds&sdk.TRANSFORMER == 0 {
				return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
			}

			return found.ProvideTransformer(makeParser(config, &found.Spec))
		},
	}
}
