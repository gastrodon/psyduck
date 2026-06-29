package datasource

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrNoValue(t *testing.T) {
	err := &ErrNoValue{Key: "missing-key"}
	assert.Equal(t, "no value for key: missing-key", err.Error())
}

func TestErrNoValueAs(t *testing.T) {
	err := &ErrNoValue{Key: "foo"}
	var target *ErrNoValue
	assert.True(t, errors.As(err, &target))
	assert.Equal(t, "foo", target.Key)
}

func TestErrNoValueAsWrapped(t *testing.T) {
	inner := &ErrNoValue{Key: "bar"}
	wrapped := fmt.Errorf("context: %w", inner)
	var target *ErrNoValue
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, "bar", target.Key)
}

func TestMapDatasource(t *testing.T) {
	ds := &mapDatasource[string]{data: map[string]string{
		"a": "alpha",
		"b": "beta",
	}}

	t.Run("exists", func(t *testing.T) {
		ok, err := ds.Exists("a")
		assert.NoError(t, err)
		assert.True(t, ok)

		ok, err = ds.Exists("missing")
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("get", func(t *testing.T) {
		v, err := ds.Get("a")
		assert.NoError(t, err)
		assert.Equal(t, "alpha", v)
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := ds.Get("nope")
		var target *ErrNoValue
		assert.True(t, errors.As(err, &target))
		assert.Equal(t, "nope", target.Key)
	})
}
