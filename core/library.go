package core

import (
	"fmt"
	"reflect"

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

func parser(spec sdk.SpecMap, config map[string]any) sdk.Parser {
	return func(target any) error {
		targetValue := reflect.ValueOf(target).Elem()
		targetType := targetValue.Type()

		for i := 0; i < targetType.NumField(); i++ {
			field := targetType.Field(i)
			fieldValue := targetValue.Field(i)

			if !fieldValue.CanSet() {
				continue
			}

			configValue, exists := config[field.Name]
			if !exists {
				continue
			}

			configValueReflect := reflect.ValueOf(configValue)
			if configValueReflect.Type().AssignableTo(fieldValue.Type()) {
				fieldValue.Set(configValueReflect)
			} else {
				return fmt.Errorf("type mismatch for field %s", field.Name)
			}
		}

		return nil
	}
}

type library struct {
	resources map[string]*sdk.Resource
}

func (l *library) Producer(name string, config map[string]any) (sdk.Producer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.PRODUCER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a producer", name)
	}

	return found.ProvideProducer(parser(found.Spec, config))
}

func (l *library) Consumer(name string, config map[string]any) (sdk.Consumer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.CONSUMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideConsumer(parser(found.Spec, config))
}

func (l *library) Transformer(name string, config map[string]any) (sdk.Transformer, error) {
	found, ok := l.resources[name]
	if !ok {
		return nil, fmt.Errorf("can't find resource %s", name)
	}

	if found.Kinds&sdk.TRANSFORMER == 0 {
		return nil, fmt.Errorf("resource %s doesn't provide a consumer", name)
	}

	return found.ProvideTransformer(parser(found.Spec, config))
}

type Library interface {
	Producer(string, map[string]any) (sdk.Producer, error)
	Consumer(string, map[string]any) (sdk.Consumer, error)
	Transformer(string, map[string]any) (sdk.Transformer, error)
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
