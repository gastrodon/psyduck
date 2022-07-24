package sdk

type Mover func(chan string) chan interface{}
type MoverProvider func(func(interface{}) error) Mover

type Producer Mover
type ProducerProvider func(func(interface{}) error) Producer

type Consumer Mover
type ConsumerProvider func(func(interface{}) error) Consumer

type Transformer func(interface{}) interface{}
type TransformerProvider func(func(interface{}) error) Transformer
