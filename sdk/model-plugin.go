package sdk

type Plugin struct {
	Name               string
	ProvideProducer    map[string]ProducerProvider
	ProvideConsumer    map[string]ConsumerProvider
	ProvideTransformer map[string]TransformerProvider
}
