package sdk

type Parser func(interface{}) error
type SpecParser func(SpecMap, interface{}) error

type Mover func(chan string, func()) (chan []byte, chan error)
type MoverProvider func(func([]byte) error) (Mover, error)

type Producer Mover
type ProducerProvider func(Parser, SpecParser) (Producer, error)

type Consumer Mover
type ConsumerProvider func(Parser, SpecParser) (Consumer, error)

type Transformer func([]byte) ([]byte, error)
type TransformerProvider func(Parser, SpecParser) (Transformer, error)
