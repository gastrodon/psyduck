package psyduck

import "github.com/gastrodon/psyduck/model"

var Plugin = model.Plugin{
	Name: "psyduck",

	ProvideProducer: map[string]model.ProducerProvider{},
	ProvideConsumer: map[string]model.ConsumerProvider{
		"psyduck_trash": consumeTrash,
	},
	ProvideTransformer: map[string]model.TransformerProvider{
		"psyduck_inspect": inspect,
	},
}
