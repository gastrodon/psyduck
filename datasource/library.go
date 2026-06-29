package datasource

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
)

// Library provides resource instantiation from loaded plugin resources.
// Unlike core.Library, its methods take an sdk.Parser directly —
// the caller (Format implementation) is responsible for building
// a format-appropriate parser.
type Library interface {
	Producer(kind string, parse sdk.Parser) (sdk.Producer, error)
	Consumer(kind string, parse sdk.Parser) (sdk.Consumer, error)
	Transformer(kind string, parse sdk.Parser) (sdk.Transformer, error)
	Spec(kind string) (sdk.SpecMap, bool)
}

type library struct {
	resources map[string]*sdk.Resource
}

func NewLibrary(plugins ...*sdk.Plugin) Library {
	resources := make(map[string]*sdk.Resource)
	for _, p := range plugins {
		for _, r := range p.Resources {
			resources[r.Name] = r
		}
	}
	return &library{resources: resources}
}

func (l *library) Spec(kind string) (sdk.SpecMap, bool) {
	r, ok := l.resources[kind]
	if !ok {
		return nil, false
	}
	return r.Spec, true
}

func (l *library) Producer(kind string, parse sdk.Parser) (sdk.Producer, error) {
	r, ok := l.resources[kind]
	if !ok {
		return nil, &ErrNoValue{Key: kind}
	}
	if r.Kinds&sdk.PRODUCER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a producer", kind)
	}
	return r.ProvideProducer(parse)
}

func (l *library) Consumer(kind string, parse sdk.Parser) (sdk.Consumer, error) {
	r, ok := l.resources[kind]
	if !ok {
		return nil, &ErrNoValue{Key: kind}
	}
	if r.Kinds&sdk.CONSUMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", kind)
	}
	return r.ProvideConsumer(parse)
}

func (l *library) Transformer(kind string, parse sdk.Parser) (sdk.Transformer, error) {
	r, ok := l.resources[kind]
	if !ok {
		return nil, &ErrNoValue{Key: kind}
	}
	if r.Kinds&sdk.TRANSFORMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a transformer", kind)
	}
	return r.ProvideTransformer(parse)
}
