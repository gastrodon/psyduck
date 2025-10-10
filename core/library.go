package core

import (
	"fmt"
	"math"
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

func isIntegerKind(kind reflect.Kind) bool {
	return kind == reflect.Int || kind == reflect.Int8 || kind == reflect.Int16 || kind == reflect.Int32 || kind == reflect.Int64 ||
		kind == reflect.Uint || kind == reflect.Uint8 || kind == reflect.Uint16 || kind == reflect.Uint32 || kind == reflect.Uint64
}

func setIntegerField(key string, fieldValue, configValueReflect reflect.Value) error {
	if configValueReflect.Type().AssignableTo(fieldValue.Type()) {
		fieldValue.Set(configValueReflect)
		return nil
	}
	if !isIntegerKind(fieldValue.Kind()) || !isIntegerKind(configValueReflect.Kind()) {
		return fmt.Errorf("type mismatch for field %s: got %s, expected %s", key, configValueReflect.Type(), fieldValue.Type())
	}
	var sourceInt int64
	var sourceUint uint64
	var sourceIsSigned bool
	switch configValueReflect.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		sourceInt = configValueReflect.Int()
		sourceIsSigned = true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		sourceUint = configValueReflect.Uint()
		sourceIsSigned = false
	}
	switch fieldValue.Kind() {
	case reflect.Int:
		if sourceIsSigned {
			fieldValue.SetInt(sourceInt)
		} else {
			if sourceUint > math.MaxInt {
				return fmt.Errorf("value %d out of range for field %s (int)", sourceUint, key)
			}
			fieldValue.SetInt(int64(sourceUint))
		}
	case reflect.Int8:
		var val int64
		if sourceIsSigned {
			val = sourceInt
		} else {
			if sourceUint > math.MaxInt8 {
				return fmt.Errorf("value %d out of range for field %s (int8)", sourceUint, key)
			}
			val = int64(sourceUint)
		}
		if val < math.MinInt8 || val > math.MaxInt8 {
			return fmt.Errorf("value %d out of range for field %s (int8)", val, key)
		}
		fieldValue.SetInt(val)
	case reflect.Int16:
		var val int64
		if sourceIsSigned {
			val = sourceInt
		} else {
			if sourceUint > math.MaxInt16 {
				return fmt.Errorf("value %d out of range for field %s (int16)", sourceUint, key)
			}
			val = int64(sourceUint)
		}
		if val < math.MinInt16 || val > math.MaxInt16 {
			return fmt.Errorf("value %d out of range for field %s (int16)", val, key)
		}
		fieldValue.SetInt(val)
	case reflect.Int32:
		var val int64
		if sourceIsSigned {
			val = sourceInt
		} else {
			if sourceUint > math.MaxInt32 {
				return fmt.Errorf("value %d out of range for field %s (int32)", sourceUint, key)
			}
			val = int64(sourceUint)
		}
		if val < math.MinInt32 || val > math.MaxInt32 {
			return fmt.Errorf("value %d out of range for field %s (int32)", val, key)
		}
		fieldValue.SetInt(val)
	case reflect.Int64:
		if sourceIsSigned {
			fieldValue.SetInt(sourceInt)
		} else {
			if sourceUint > math.MaxInt64 {
				return fmt.Errorf("value %d out of range for field %s (int64)", sourceUint, key)
			}
			fieldValue.SetInt(int64(sourceUint))
		}
	case reflect.Uint:
		var val uint64
		if sourceIsSigned {
			if sourceInt < 0 {
				return fmt.Errorf("negative value %d for field %s (uint)", sourceInt, key)
			}
			val = uint64(sourceInt)
		} else {
			val = sourceUint
		}
		if val > math.MaxUint {
			return fmt.Errorf("value %d out of range for field %s (uint)", val, key)
		}
		fieldValue.SetUint(val)
	case reflect.Uint8:
		var val uint64
		if sourceIsSigned {
			if sourceInt < 0 || sourceInt > math.MaxUint8 {
				return fmt.Errorf("value %d out of range for field %s (uint8)", sourceInt, key)
			}
			val = uint64(sourceInt)
		} else {
			if sourceUint > math.MaxUint8 {
				return fmt.Errorf("value %d out of range for field %s (uint8)", sourceUint, key)
			}
			val = sourceUint
		}
		fieldValue.SetUint(val)
	case reflect.Uint16:
		var val uint64
		if sourceIsSigned {
			if sourceInt < 0 || sourceInt > math.MaxUint16 {
				return fmt.Errorf("value %d out of range for field %s (uint16)", sourceInt, key)
			}
			val = uint64(sourceInt)
		} else {
			if sourceUint > math.MaxUint16 {
				return fmt.Errorf("value %d out of range for field %s (uint16)", sourceUint, key)
			}
			val = sourceUint
		}
		fieldValue.SetUint(val)
	case reflect.Uint32:
		var val uint64
		if sourceIsSigned {
			if sourceInt < 0 || sourceInt > math.MaxUint32 {
				return fmt.Errorf("value %d out of range for field %s (uint32)", sourceInt, key)
			}
			val = uint64(sourceInt)
		} else {
			if sourceUint > math.MaxUint32 {
				return fmt.Errorf("value %d out of range for field %s (uint32)", sourceUint, key)
			}
			val = sourceUint
		}
		fieldValue.SetUint(val)
	case reflect.Uint64:
		if sourceIsSigned {
			if sourceInt < 0 {
				return fmt.Errorf("negative value %d for field %s (uint64)", sourceInt, key)
			}
			fieldValue.SetUint(uint64(sourceInt))
		} else {
			fieldValue.SetUint(sourceUint)
		}
	}
	return nil
}

func parser(spec sdk.SpecMap, config map[string]any) sdk.Parser {
	return func(target any) error {
		targetValue := reflect.ValueOf(target).Elem()
		targetType := targetValue.Type()

		for key, configValue := range config {
			// find field with tag "psy" == key
			var fieldValue reflect.Value
			found := false
			for i := 0; i < targetType.NumField(); i++ {
				f := targetType.Field(i)
				if f.Tag.Get("psy") == key {
					fieldValue = targetValue.Field(i)
					found = true
					break
				}
			}
			if !found {
				continue
			}
			if !fieldValue.CanSet() {
				continue
			}

			configValueReflect := reflect.ValueOf(configValue)
			if err := setIntegerField(key, fieldValue, configValueReflect); err != nil {
				return err
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
