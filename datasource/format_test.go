package datasource

import (
	"errors"
	"testing"

	"github.com/psyduck-etl/sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

// mockFormat implements the Format interface with configurable return values.
type mockFormat struct {
	plugins    []*sdk.Plugin
	pluginsErr error

	values    map[string]cty.Value
	valuesErr error

	resources    *ResourceSources
	resourcesErr error

	// capturedPlugins records the plugins passed to Resources so tests can inspect them.
	capturedPlugins []*sdk.Plugin
}

func (m *mockFormat) Plugins() ([]*sdk.Plugin, error) {
	return m.plugins, m.pluginsErr
}

func (m *mockFormat) Values() (map[string]cty.Value, error) {
	return m.values, m.valuesErr
}

func (m *mockFormat) Resources(plugins []*sdk.Plugin) (*ResourceSources, error) {
	m.capturedPlugins = plugins
	return m.resources, m.resourcesErr
}

func TestConfigDatasources(t *testing.T) {
	userProducer := func(send chan<- []byte, errs chan<- error) {
		send <- []byte("hello")
		close(send)
	}
	userConsumer := func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		for range recv {
		}
		done <- struct{}{}
	}
	userTransformer := func(in []byte) ([]byte, error) {
		return append(in, '!'), nil
	}

	userPlugin := &sdk.Plugin{
		Name: "user-plugin",
		Resources: []*sdk.Resource{
			{Name: "user-res", Kinds: sdk.PRODUCER},
		},
	}

	mf := &mockFormat{
		plugins: []*sdk.Plugin{userPlugin},
		values: map[string]cty.Value{
			"greeting": cty.StringVal("hi"),
			"count":    cty.NumberIntVal(42),
		},
		resources: &ResourceSources{
			Producers:    map[string]ProducerSet{"p1": LiteralProducerSet(userProducer)},
			Consumers:    map[string]ConsumerSet{"c1": LiteralConsumerSet(userConsumer)},
			Transformers: map[string]sdk.Transformer{"t1": userTransformer},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)
	require.NotNil(t, sources)

	// Verify Values datasource
	ok, err := sources.Values().Exists("greeting")
	require.NoError(t, err)
	assert.True(t, ok)

	v, err := sources.Values().Get("greeting")
	require.NoError(t, err)
	assert.Equal(t, cty.StringVal("hi"), v)

	v, err = sources.Values().Get("count")
	require.NoError(t, err)
	assert.Equal(t, cty.NumberIntVal(42), v)

	ok, err = sources.Values().Exists("missing")
	require.NoError(t, err)
	assert.False(t, ok)

	// Verify Plugins datasource includes user plugin
	ok, err = sources.Plugins().Exists("user-plugin")
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := sources.Plugins().Get("user-plugin")
	require.NoError(t, err)
	assert.Equal(t, userPlugin, got)

	// Verify Plugins datasource includes stdlib plugin ("psyduck")
	ok, err = sources.Plugins().Exists("psyduck")
	require.NoError(t, err)
	assert.True(t, ok, "stdlib plugin 'psyduck' should be auto-included")

	stdlibPlugin, err := sources.Plugins().Get("psyduck")
	require.NoError(t, err)
	assert.Equal(t, "psyduck", stdlibPlugin.Name)

	// Verify Producers datasource
	ok, err = sources.Producers().Exists("p1")
	require.NoError(t, err)
	assert.True(t, ok)

	ps, err := sources.Producers().Get("p1")
	require.NoError(t, err)
	require.NotNil(t, ps)

	// Verify Consumers datasource
	ok, err = sources.Consumers().Exists("c1")
	require.NoError(t, err)
	assert.True(t, ok)

	cs, err := sources.Consumers().Get("c1")
	require.NoError(t, err)
	require.NotNil(t, cs)

	// Verify Transformers datasource
	ok, err = sources.Transformers().Exists("t1")
	require.NoError(t, err)
	assert.True(t, ok)

	tf, err := sources.Transformers().Get("t1")
	require.NoError(t, err)
	require.NotNil(t, tf)

	out, err := tf([]byte("data"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data!"), out)

	// Verify Env datasource is non-nil (basic smoke check)
	require.NotNil(t, sources.Env())
}

func TestConfigDatasources_PluginsError(t *testing.T) {
	pluginsErr := errors.New("failed to load plugins")
	mf := &mockFormat{
		pluginsErr: pluginsErr,
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.Error(t, err)
	assert.Equal(t, pluginsErr, err)
}

func TestConfigDatasources_ValuesError(t *testing.T) {
	valuesErr := errors.New("failed to parse values")
	mf := &mockFormat{
		plugins:   []*sdk.Plugin{},
		valuesErr: valuesErr,
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.Error(t, err)
	assert.Equal(t, valuesErr, err)
}

func TestConfigDatasources_ResourcesError(t *testing.T) {
	resourcesErr := errors.New("failed to resolve resources")
	mf := &mockFormat{
		plugins:      []*sdk.Plugin{},
		values:       map[string]cty.Value{},
		resourcesErr: resourcesErr,
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.Error(t, err)
	assert.Equal(t, resourcesErr, err)
}

func TestConfigDatasources_EmptyFormat(t *testing.T) {
	mf := &mockFormat{
		plugins: []*sdk.Plugin{},
		values:  map[string]cty.Value{},
		resources: &ResourceSources{
			Producers:    map[string]ProducerSet{},
			Consumers:    map[string]ConsumerSet{},
			Transformers: map[string]sdk.Transformer{},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)
	require.NotNil(t, sources)

	// Even with no user plugins, stdlib ("psyduck") should be accessible
	ok, err := sources.Plugins().Exists("psyduck")
	require.NoError(t, err)
	assert.True(t, ok, "stdlib plugin should be available even with empty user plugins")

	stdlibPlugin, err := sources.Plugins().Get("psyduck")
	require.NoError(t, err)
	assert.Equal(t, "psyduck", stdlibPlugin.Name)
	assert.True(t, len(stdlibPlugin.Resources) > 0, "stdlib plugin should have resources")

	// Producers, Consumers, Transformers should be valid but empty
	ok, err = sources.Producers().Exists("anything")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = sources.Consumers().Exists("anything")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = sources.Transformers().Exists("anything")
	require.NoError(t, err)
	assert.False(t, ok)

	// Values should be valid but empty
	ok, err = sources.Values().Exists("anything")
	require.NoError(t, err)
	assert.False(t, ok)

	// Env should be non-nil
	require.NotNil(t, sources.Env())
}

func TestConfigDatasources_PluginsIncludesBothUserAndStdlib(t *testing.T) {
	userPlugin := &sdk.Plugin{
		Name: "my-plugin",
		Resources: []*sdk.Resource{
			{
				Name:  "my-resource",
				Kinds: sdk.PRODUCER,
				ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
					return func(send chan<- []byte, errs chan<- error) { close(send) }, nil
				},
			},
		},
	}

	mf := &mockFormat{
		plugins: []*sdk.Plugin{userPlugin},
		values:  map[string]cty.Value{},
		resources: &ResourceSources{
			Producers:    map[string]ProducerSet{},
			Consumers:    map[string]ConsumerSet{},
			Transformers: map[string]sdk.Transformer{},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	_, err := cfg.Datasources()
	require.NoError(t, err)

	plugins := mf.capturedPlugins
	require.NotNil(t, plugins, "plugins should have been passed to Resources")

	names := make(map[string]bool)
	for _, p := range plugins {
		names[p.Name] = true
	}
	assert.True(t, names["my-plugin"], "user plugin should be passed to Resources")
	assert.True(t, names["psyduck"], "stdlib plugin should be passed to Resources")
}
