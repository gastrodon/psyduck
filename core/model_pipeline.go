package core

import (
	"github.com/gastrodon/psyduck/sdk"
)

type Pipeline struct {
	Producer           sdk.Producer
	Consumer           sdk.Consumer
	StackedTransformer sdk.Transformer
}

type Pipelines map[string]*Pipeline
