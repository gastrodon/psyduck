package core

import (
	"github.com/gastrodon/psyduck/sdk"
)

type Pipeline struct {
	Producer           sdk.Producer
	Consumer           sdk.Consumer
	Transformers       []sdk.Transformer
	StackedTransformer sdk.Transformer
}
