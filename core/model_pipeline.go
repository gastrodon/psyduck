package core

import (
	"github.com/psyduck-std/sdk"
)

type Pipeline struct {
	Producer           sdk.Producer
	Consumer           sdk.Consumer
	StackedTransformer sdk.Transformer
}

type Pipelines map[string]*Pipeline
