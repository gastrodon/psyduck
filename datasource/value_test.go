package datasource

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestValueGetString(t *testing.T) {
	ds, err := Value("test.hcl", []byte(`value { foo = "bar" }`))
	require.NoError(t, err)

	v, err := ds.Get("foo")
	require.NoError(t, err)
	assert.Equal(t, cty.StringVal("bar"), v)
}

func TestValueExistsTrue(t *testing.T) {
	ds, err := Value("test.hcl", []byte(`value { foo = "bar" }`))
	require.NoError(t, err)

	ok, err := ds.Exists("foo")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestValueExistsFalse(t *testing.T) {
	ds, err := Value("test.hcl", []byte(`value { foo = "bar" }`))
	require.NoError(t, err)

	ok, err := ds.Exists("missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestValueGetAbsentKey(t *testing.T) {
	ds, err := Value("test.hcl", []byte(`value { foo = "bar" }`))
	require.NoError(t, err)

	v, err := ds.Get("missing")
	assert.Equal(t, cty.NilVal, v)

	var target *ErrNoValue
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "missing", target.Key)
}

func TestValueMultipleBlocks(t *testing.T) {
	hcl := []byte("value { a = \"1\" }\nvalue { b = \"2\" }")
	ds, err := Value("test.hcl", hcl)
	require.NoError(t, err)

	a, err := ds.Get("a")
	require.NoError(t, err)
	assert.Equal(t, cty.StringVal("1"), a)

	b, err := ds.Get("b")
	require.NoError(t, err)
	assert.Equal(t, cty.StringVal("2"), b)
}

func TestValueDuplicateKeyError(t *testing.T) {
	hcl := []byte("value { k = \"1\" }\nvalue { k = \"2\" }")
	_, err := Value("test.hcl", hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValueNumber(t *testing.T) {
	ds, err := Value("test.hcl", []byte(`value { n = 42 }`))
	require.NoError(t, err)

	v, err := ds.Get("n")
	require.NoError(t, err)
	assert.True(t, v.RawEquals(cty.NumberIntVal(42)))
}
