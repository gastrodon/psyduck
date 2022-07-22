package core

import (
	"fmt"

	"github.com/gastrodon/psyduck/model"
)

func NewLibrary() model.Library {
	lookupProducer := make(map[string]model.ProducerProvider)
	lookupConsumer := make(map[string]model.ConsumerProvider)
	lookupTransformer := make(map[string]model.TransformerProvider)

	return model.Library{
		Load: func(plugin model.Plugin) {
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
		ProvideProducer: func(name string, config interface{}) model.Producer {
			found, ok := lookupProducer[name]
			if !ok {
				panic(fmt.Sprintf("can't find producer %s!", name))
			}

			return found(config)
		},
		ProvideConsumer: func(name string, config interface{}) model.Consumer {
			found, ok := lookupConsumer[name]
			if !ok {
				panic(fmt.Sprintf("can't find consumer %s!", name))
			}

			return found(config)
		},
		ProvideTransformer: func(name string, config interface{}) model.Transformer {
			found, ok := lookupTransformer[name]
			if !ok {
				panic(fmt.Sprintf("can't find transformer %s!", name))
			}

			return found(config)
		},
	}
}
