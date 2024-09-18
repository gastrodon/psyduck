package core

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib"
)

func makeBodySchema(specMap sdk.SpecMap) *hcl.BodySchema {
	attributes := make([]hcl.AttributeSchema, len(specMap))

	index := 0
	for _, spec := range specMap {
		attributes[index] = hcl.AttributeSchema{
			Name:     spec.Name,
			Required: spec.Required,
		}

		index++
	}

	return &hcl.BodySchema{
		Attributes: attributes,
	}
}

func parser(spec sdk.SpecMap, evalCtx *hcl.EvalContext, config hcl.Body) sdk.Parser {
	return func(target interface{}) error {
		content, _, diags := config.PartialContent(makeBodySchema(spec))
		if diags.HasErrors() {
			return diags
		}

		if diags := decodeAttributes(spec, evalCtx, content.Attributes, target); diags.HasErrors() {
			return diags
		}

		return nil
	}
}

type library struct {
	resources map[string]*sdk.Resource
}

func (l *library) Producer(name string, ctx *hcl.EvalContext, body hcl.Body) (sdk.Producer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.PRODUCER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
	}

	return found.ProvideProducer(parser(found.Spec, ctx, body))
}

func (l *library) Consumer(name string, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Consumer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.CONSUMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideConsumer(parser(found.Spec, evalCtx, config))
}

func (l *library) Transformer(name string, evalCtx *hcl.EvalContext, config hcl.Body) (sdk.Transformer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.TRANSFORMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideTransformer(parser(found.Spec, evalCtx, config))
}

type Library interface {
	Producer(string, *hcl.EvalContext, hcl.Body) (sdk.Producer, error)
	Consumer(string, *hcl.EvalContext, hcl.Body) (sdk.Consumer, error)
	Transformer(string, *hcl.EvalContext, hcl.Body) (sdk.Transformer, error)
}

func NewLibrary(plugins []*sdk.Plugin) Library {
	lookupResource := make(map[string]*sdk.Resource)
	for _, plugin := range append(plugins, stdlib.Plugin()) {
		for _, resource := range plugin.Resources {
			lookupResource[resource.Name] = resource
		}
	}

	return &library{lookupResource}
}
