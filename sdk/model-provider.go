package sdk

type Mover func(chan string) (chan []byte, chan error)
type MoverProvider func(func([]byte) error) (Mover, error)

type Producer Mover
type ProducerProvider func(func(interface{}) error) (Producer, error)

type Consumer Mover
type ConsumerProvider func(func(interface{}) error) (Consumer, error)

type Transformer func([]byte) ([]byte, error)
type TransformerProvider func(func(interface{}) error) (Transformer, error)
