package datasource

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvExistsTrue(t *testing.T) {
	t.Setenv("PSYDUCK_TEST_EXISTS", "1")
	ds := Env()
	ok, err := ds.Exists("PSYDUCK_TEST_EXISTS")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestEnvExistsFalse(t *testing.T) {
	ds := Env()
	ok, err := ds.Exists("PSYDUCK_TEST_DOES_NOT_EXIST_EVER")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestEnvGetValue(t *testing.T) {
	t.Setenv("PSYDUCK_TEST_GET", "hello")
	ds := Env()
	val, err := ds.Get("PSYDUCK_TEST_GET")
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestEnvGetMissing(t *testing.T) {
	ds := Env()
	val, err := ds.Get("PSYDUCK_TEST_DOES_NOT_EXIST_EVER")
	assert.Equal(t, "", val)
	var target *ErrNoValue
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "PSYDUCK_TEST_DOES_NOT_EXIST_EVER", target.Key)
}
