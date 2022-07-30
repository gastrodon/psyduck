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
		fmt.Println(diags)

		return gocty.FromCtyValue(decoded, target)
	}
}

func NewLibrary() *Library {
	lookupProducer := make(map[string]sdk.ProducerProvider)
	lookupConsumer := make(map[string]sdk.ConsumerProvider)
	lookupTransformer := make(map[string]sdk.TransformerProvider)
	lookupSpec := make(map[string]*hcldec.ObjectSpec)

	return &Library{
		Load: func(plugin *sdk.Plugin) {
			for name, provide := range plugin.ProvideProducer {
				lookupProducer[name] = provide
			}
			for name, provide := range plugin.ProvideConsumer {
				lookupConsumer[name] = provide
			}
			for name, provide := range plugin.ProvideTransformer {
				lookupTransformer[name] = provide
			}
		},
		ProvideProducer: func(name string, config hcl.Body) (sdk.Producer, error) {
			found, ok := lookupProducer[name]
			if !ok {
				return nil, fmt.Errorf("can't find producer %s", name)
			}

			spec, ok := lookupSpec[name]
			if !ok {
				return nil, fmt.Errorf("can't find spec %s", name)
			}

			return found(makeParser(config, spec))
		},
		ProvideConsumer: func(name string, config hcl.Body) (sdk.Consumer, error) {
			found, ok := lookupConsumer[name]
			if !ok {
				return nil, fmt.Errorf("can't find consumer %s", name)
			}

			spec, ok := lookupSpec[name]
			if !ok {
				return nil, fmt.Errorf("can't find spec %s", name)
			}

			return found(makeParser(config, spec))
		},
		ProvideTransformer: func(name string, config hcl.Body) (sdk.Transformer, error) {
			found, ok := lookupTransformer[name]
			if !ok {
				return nil, fmt.Errorf("can't find transformer %s", name)
			}

			spec, ok := lookupSpec[name]
			if !ok {
				return nil, fmt.Errorf("can't find spec %s", name)
			}

			return found(makeParser(config, spec))
		},
	}
}
