package model

type Transformer func(interface{}) interface{}
type TransformerProvider func(func(interface{}) error) Transformer
