package datasource

import (
	"errors"
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginExists(t *testing.T) {
	p := &sdk.Plugin{Name: "test-plugin", Resources: []*sdk.Resource{{Name: "res1"}}}
	ds := &mapDatasource[*sdk.Plugin]{data: map[string]*sdk.Plugin{"test-plugin": p}}

	ok, err := ds.Exists("test-plugin")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = ds.Exists("missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPluginGet(t *testing.T) {
	p := &sdk.Plugin{Name: "test-plugin", Resources: []*sdk.Resource{{Name: "res1"}}}
	ds := &mapDatasource[*sdk.Plugin]{data: map[string]*sdk.Plugin{"test-plugin": p}}

	got, err := ds.Get("test-plugin")
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestPluginGetMissing(t *testing.T) {
	ds := &mapDatasource[*sdk.Plugin]{data: map[string]*sdk.Plugin{}}

	got, err := ds.Get("missing")
	assert.Nil(t, got)
	var target *ErrNoValue
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "missing", target.Key)
}

func TestPluginMultiple(t *testing.T) {
	p1 := &sdk.Plugin{Name: "alpha", Resources: []*sdk.Resource{{Name: "a1"}}}
	p2 := &sdk.Plugin{Name: "beta", Resources: []*sdk.Resource{{Name: "b1"}, {Name: "b2"}}}
	ds := &mapDatasource[*sdk.Plugin]{data: map[string]*sdk.Plugin{"alpha": p1, "beta": p2}}

	got1, err := ds.Get("alpha")
	require.NoError(t, err)
	assert.Equal(t, p1, got1)

	got2, err := ds.Get("beta")
	require.NoError(t, err)
	assert.Equal(t, p2, got2)

	ok, err := ds.Exists("alpha")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = ds.Exists("beta")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = ds.Exists("gamma")
	require.NoError(t, err)
	assert.False(t, ok)
}
