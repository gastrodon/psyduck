package model

type Transformer func(interface{}) interface{}
type TransformerProvider func(interface{}) Transformer
