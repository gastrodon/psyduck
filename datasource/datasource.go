package datasource

import "github.com/psyduck-etl/sdk"

type ErrNoValue struct {
	Key string
}

func (e *ErrNoValue) Error() string {
	return "no value for key: " + e.Key
}

// Datasource provides typed, key-value access to a particular kind of
// configuration data. Implementations are specific to a backing store
// (env vars, HCL blocks, plugin loader, etc.) but callers interact
// only through this interface.
type Datasource[T any] interface {
	// Exists reports whether a value is available for key.
	Exists(key string) (bool, error)

	// Get returns the value for key, or *ErrNoValue if the key is absent.
	Get(key string) (T, error)
}

// ProducerSet yields sdk.Producers in chunks. Returns up to max
// producers. A nil slice with nil error signals exhaustion.
type ProducerSet func(max int) ([]sdk.Producer, error)

// ConsumerSet yields sdk.Consumers in chunks. Returns up to max
// consumers. A nil slice with nil error signals exhaustion.
type ConsumerSet func(max int) ([]sdk.Consumer, error)

// mapDatasource is a generic Datasource backed by a map.
type mapDatasource[T any] struct {
	data map[string]T
}

func (d *mapDatasource[T]) Exists(key string) (bool, error) {
	_, ok := d.data[key]
	return ok, nil
}

func (d *mapDatasource[T]) Get(key string) (T, error) {
	v, ok := d.data[key]
	if !ok {
		var zero T
		return zero, &ErrNoValue{Key: key}
	}
	return v, nil
}
