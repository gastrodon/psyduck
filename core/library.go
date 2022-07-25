package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
	"github.com/mitchellh/mapstructure"
)

func makeParser(data map[string]interface{}) func(interface{}) error {
	return func(target interface{}) error {
		decodeConfig := &mapstructure.DecoderConfig{
			Metadata: nil,
			Result:   target,
			TagName:  "psy",
		}

		decoder, err := mapstructure.NewDecoder(decodeConfig)
		if err != nil {
			return err
		}

		return decoder.Decode(data)
	}
}

func NewLibrary() *Library {
	lookupProducer := make(map[string]sdk.ProducerProvider)
	lookupConsumer := make(map[string]sdk.ConsumerProvider)
	lookupTransformer := make(map[string]sdk.TransformerProvider)

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
		ProvideProducer: func(name string, config map[string]interface{}) sdk.Producer {
			found, ok := lookupProducer[name]
			if !ok {
				panic(fmt.Sprintf("can't find producer %s!", name))
			}

			return found(makeParser(config))
		},
		ProvideConsumer: func(name string, config map[string]interface{}) sdk.Consumer {
			found, ok := lookupConsumer[name]
			if !ok {
				panic(fmt.Sprintf("can't find consumer %s!", name))
			}

			return found(makeParser(config))
		},
		ProvideTransformer: func(name string, config map[string]interface{}) sdk.Transformer {
			found, ok := lookupTransformer[name]
			if !ok {
				panic(fmt.Sprintf("can't find transformer %s!", name))
			}

			return found(makeParser(config))
		},
	}
}
