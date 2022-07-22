package model

type Plugin struct {
	Name               string
	ProvideProducer    map[string]ProducerProvider
	ProvideConsumer    map[string]ConsumerProvider
	ProvideTransformer map[string]TransformerProvider
}

type Library struct {
	Load               func(Plugin)
	ProvideProducer    func(string, interface{}) Producer
	ProvideConsumer    func(string, interface{}) Consumer
	ProvideTransformer func(string, interface{}) Transformer
}
