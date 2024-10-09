package core

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/gocty"

	"github.com/gastrodon/psyduck/stdlib"
)

type library struct {
	plugins   []*sdk.Plugin
	resources map[string]*sdk.Resource
	functions map[string]function.Function
}

func (l *library) Producer(name string, options map[string]cty.Value) (sdk.Producer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.PRODUCER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
	}

	return found.ProvideProducer(func(target interface{}) error {
		return gocty.FromCtyValue(cty.ObjectVal(options), target)
	})
}

func (l *library) Consumer(name string, options map[string]cty.Value) (sdk.Consumer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.CONSUMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideConsumer(func(target interface{}) error {
		return gocty.FromCtyValue(cty.ObjectVal(options), target)
	})
}

func (l *library) Transformer(name string, options map[string]cty.Value) (sdk.Transformer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.TRANSFORMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideTransformer(func(target interface{}) error {
		return gocty.FromCtyValue(cty.ObjectVal(options), target)
	})
}

func (l *library) Ctx() *hcl.EvalContext {
	ctx := &hcl.EvalContext{}
	for _, plugin := range l.plugins {
		for k, v := range plugin.Ctx().Variables {
			ctx.Variables[k] = v
		}
	}

	ctx.Functions = make(map[string]function.Function)
	for name, function := range l.functions {
		ctx.Functions[name] = function
	}

	return ctx
}

type Library interface {
	Producer(string, map[string]cty.Value) (sdk.Producer, error)
	Consumer(string, map[string]cty.Value) (sdk.Consumer, error)
	Transformer(string, map[string]cty.Value) (sdk.Transformer, error)
	Ctx() *hcl.EvalContext
}

func NewLibrary(plugins []*sdk.Plugin) Library {
	lookupResource := make(map[string]*sdk.Resource)
	fns := make(map[string]function.Function)
	for _, plugin := range append(plugins, stdlib.Plugin()) {
		for _, resource := range plugin.Resources {
			lookupResource[resource.Name] = resource
		}

		for name, function := range plugin.Functions {
			fns[name] = function
		}
	}

	return &library{plugins, lookupResource, fns}
}
