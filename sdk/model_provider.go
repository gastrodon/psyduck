package sdk

type signalKind int
type Signal chan signalKind

const (
	SIG_INVALID = iota
	SIG_DONE
)

type Parser func(interface{}) error
type SpecParser func(SpecMap, interface{}) error

type Producer func(Signal, func()) (chan []byte, chan error)
type ProducerProvider func(Parser, SpecParser) (Producer, error)

type Consumer func(Signal, func()) (chan []byte, chan error)
type ConsumerProvider func(Parser, SpecParser) (Consumer, error)

type Transformer func([]byte) ([]byte, error)
type TransformerProvider func(Parser, SpecParser) (Transformer, error)
