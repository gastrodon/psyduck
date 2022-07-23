package psyduck

import "github.com/gastrodon/psyduck/model"

func Plugin() *model.Plugin {
	return &model.Plugin{
		Name: "psyduck",

		ProvideProducer: map[string]model.ProducerProvider{},
		ProvideConsumer: map[string]model.ConsumerProvider{
			"psyduck-trash": consumeTrash,
		},
		ProvideTransformer: map[string]model.TransformerProvider{
			"psyduck-inspect": inspect,
		},
	}
}
