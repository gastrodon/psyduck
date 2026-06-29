package datasource

import (
	"errors"
	"testing"

	"github.com/gastrodon/psyduck/stdlib"
	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopParser(v interface{}) error { return nil }

func TestLibrarySpec(t *testing.T) {
	lib := NewLibrary(stdlib.Plugin())

	t.Run("known resource", func(t *testing.T) {
		spec, ok := lib.Spec("constant")
		assert.True(t, ok)
		assert.NotNil(t, spec)
		_, hasValue := spec["value"]
		assert.True(t, hasValue)
	})

	t.Run("unknown resource", func(t *testing.T) {
		spec, ok := lib.Spec("does-not-exist")
		assert.False(t, ok)
		assert.Nil(t, spec)
	})

	t.Run("resource with no spec", func(t *testing.T) {
		spec, ok := lib.Spec("trash")
		assert.True(t, ok)
		assert.Nil(t, spec)
	})
}

func TestLibraryProducer(t *testing.T) {
	lib := NewLibrary(stdlib.Plugin())

	t.Run("success", func(t *testing.T) {
		p, err := lib.Producer("constant", noopParser)
		require.NoError(t, err)
		assert.NotNil(t, p)
	})

	t.Run("unknown resource", func(t *testing.T) {
		p, err := lib.Producer("does-not-exist", noopParser)
		assert.Nil(t, p)
		require.Error(t, err)

		var target *ErrNoValue
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "does-not-exist", target.Key)
	})

	t.Run("wrong kind", func(t *testing.T) {
		p, err := lib.Producer("trash", noopParser)
		assert.Nil(t, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't provide a producer")
	})
}

func TestLibraryConsumer(t *testing.T) {
	lib := NewLibrary(stdlib.Plugin())

	t.Run("success", func(t *testing.T) {
		c, err := lib.Consumer("trash", noopParser)
		require.NoError(t, err)
		assert.NotNil(t, c)
	})

	t.Run("unknown resource", func(t *testing.T) {
		c, err := lib.Consumer("does-not-exist", noopParser)
		assert.Nil(t, c)
		require.Error(t, err)

		var target *ErrNoValue
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "does-not-exist", target.Key)
	})

	t.Run("wrong kind", func(t *testing.T) {
		c, err := lib.Consumer("constant", noopParser)
		assert.Nil(t, c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't provide a consumer")
	})
}

func TestLibraryTransformer(t *testing.T) {
	lib := NewLibrary(stdlib.Plugin())

	t.Run("success", func(t *testing.T) {
		tr, err := lib.Transformer("inspect", noopParser)
		require.NoError(t, err)
		assert.NotNil(t, tr)
	})

	t.Run("unknown resource", func(t *testing.T) {
		tr, err := lib.Transformer("does-not-exist", noopParser)
		assert.Nil(t, tr)
		require.Error(t, err)

		var target *ErrNoValue
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "does-not-exist", target.Key)
	})

	t.Run("wrong kind", func(t *testing.T) {
		tr, err := lib.Transformer("constant", noopParser)
		assert.Nil(t, tr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't provide a transformer")
	})
}

func TestLibraryNewLibraryMergesPlugins(t *testing.T) {
	p1 := &sdk.Plugin{
		Name: "first",
		Resources: []*sdk.Resource{
			{Name: "alpha", Kinds: sdk.PRODUCER, ProvideProducer: func(sdk.Parser) (sdk.Producer, error) { return nil, nil }},
		},
	}
	p2 := &sdk.Plugin{
		Name: "second",
		Resources: []*sdk.Resource{
			{Name: "beta", Kinds: sdk.CONSUMER, ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) { return nil, nil }},
		},
	}

	lib := NewLibrary(p1, p2)

	_, ok := lib.Spec("alpha")
	assert.True(t, ok, "resource from first plugin should be present")

	_, ok = lib.Spec("beta")
	assert.True(t, ok, "resource from second plugin should be present")

	_, ok = lib.Spec("gamma")
	assert.False(t, ok, "unknown resource should not be present")
}

func TestLibraryErrNoValue(t *testing.T) {
	lib := NewLibrary()

	methods := []struct {
		name string
		call func() error
	}{
		{"Producer", func() error { _, err := lib.Producer("missing", noopParser); return err }},
		{"Consumer", func() error { _, err := lib.Consumer("missing", noopParser); return err }},
		{"Transformer", func() error { _, err := lib.Transformer("missing", noopParser); return err }},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			err := m.call()
			require.Error(t, err)

			var target *ErrNoValue
			require.True(t, errors.As(err, &target))
			assert.Equal(t, "missing", target.Key)
		})
	}
}
