package ifunny

import "github.com/gastrodon/psyduck/model"

var Plugin = model.Plugin{
	Name: "ifunny",
	ProvideProducer: map[string]model.ProducerProvider{
		"ifunny_feed_features": produceFeatures,
	},
	ProvideConsumer:    map[string]model.ConsumerProvider{},
	ProvideTransformer: map[string]model.TransformerProvider{},
}
