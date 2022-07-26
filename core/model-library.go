package core

import (
	"github.com/gastrodon/psyduck/sdk"
)

type Library struct {
	Load               func(*sdk.Plugin)
	ProvideProducer    func(string, map[string]interface{}) (sdk.Producer, error)
	ProvideConsumer    func(string, map[string]interface{}) (sdk.Consumer, error)
	ProvideTransformer func(string, map[string]interface{}) (sdk.Transformer, error)
}
