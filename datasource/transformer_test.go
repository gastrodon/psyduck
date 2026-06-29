package datasource

import (
	"fmt"
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appendTransformer(suffix string) sdk.Transformer {
	return func(data []byte) ([]byte, error) {
		return append(data, []byte(suffix)...), nil
	}
}

func TestComposeTransformersIdentity(t *testing.T) {
	tf := ComposeTransformers()

	out, err := tf([]byte("passthrough"))
	require.NoError(t, err)
	assert.Equal(t, []byte("passthrough"), out)
}

func TestComposeTransformersSingle(t *testing.T) {
	tf := ComposeTransformers(appendTransformer("!"))

	out, err := tf([]byte("hi"))
	require.NoError(t, err)
	assert.Equal(t, []byte("hi!"), out)
}

func TestComposeTransformersChain(t *testing.T) {
	tf := ComposeTransformers(appendTransformer("a"), appendTransformer("b"))

	out, err := tf([]byte("x"))
	require.NoError(t, err)
	assert.Equal(t, []byte("xab"), out)
}

func TestComposeTransformersFilterNil(t *testing.T) {
	called := false
	filter := func(data []byte) ([]byte, error) { return nil, nil }
	spy := func(data []byte) ([]byte, error) {
		called = true
		return data, nil
	}

	tf := ComposeTransformers(filter, spy)

	out, err := tf([]byte("anything"))
	require.NoError(t, err)
	assert.Nil(t, out)
	assert.False(t, called, "second transformer should not be called after nil filter")
}

func TestComposeTransformersError(t *testing.T) {
	called := false
	boom := fmt.Errorf("boom")
	failing := func(data []byte) ([]byte, error) { return nil, boom }
	spy := func(data []byte) ([]byte, error) {
		called = true
		return data, nil
	}

	tf := ComposeTransformers(failing, spy)

	out, err := tf([]byte("anything"))
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, out)
	assert.False(t, called, "second transformer should not be called after error")
}
