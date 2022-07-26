package sdk

type Mover func(chan string) chan []byte
type MoverProvider func(func([]byte) error) Mover

type Producer Mover
type ProducerProvider func(func(interface{}) error) Producer

type Consumer Mover
type ConsumerProvider func(func(interface{}) error) Consumer

type Transformer func([]byte) []byte
type TransformerProvider func(func(interface{}) error) Transformer
