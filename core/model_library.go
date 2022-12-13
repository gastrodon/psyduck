package core

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
)

type Library struct {
	Load               func(*sdk.Plugin)
	Spec               func(string) (*hcldec.ObjectSpec, error)
	ProvideProducer    func(string, *hcl.EvalContext, hcl.Body) (sdk.Producer, error)
	ProvideConsumer    func(string, *hcl.EvalContext, hcl.Body) (sdk.Consumer, error)
	ProvideTransformer func(string, *hcl.EvalContext, hcl.Body) (sdk.Transformer, error)
}

type SpecLibrary struct {
}
