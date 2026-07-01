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

	resources    *ResourceSources
	resourcesErr error

	pipelines    map[string]PipelineDecl
	pipelinesErr error

	// capturedPlugins records the plugins passed to Resources so tests can inspect them.
	capturedPlugins []*sdk.Plugin
}

func (m *mockFormat) Plugins() ([]*sdk.Plugin, error) {
	return m.plugins, m.pluginsErr
}

func (m *mockFormat) Values() (map[string]cty.Value, error) {
	return nil, nil
}

func (m *mockFormat) Resources(plugins []*sdk.Plugin) (*ResourceSources, error) {
	m.capturedPlugins = plugins
	return m.resources, m.resourcesErr
}

func (m *mockFormat) Pipelines() (map[string]PipelineDecl, error) {
	return m.pipelines, m.pipelinesErr
}

// helpers

func makeResource(name string) *sdk.Resource {
	return &sdk.Resource{
		Name:  name,
		Kinds: sdk.PRODUCER | sdk.CONSUMER | sdk.TRANSFORMER,
		ProvideProducer: func(parse sdk.Parser) (sdk.Producer, error) {
			return func(send chan<- []byte, errs chan<- error) { close(send) }, nil
		},
		ProvideConsumer: func(parse sdk.Parser) (sdk.Consumer, error) {
			return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
				for range recv {
				}
				close(done)
			}, nil
		},
		ProvideTransformer: func(parse sdk.Parser) (sdk.Transformer, error) {
			return func(in []byte) ([]byte, error) { return in, nil }, nil
		},
	}
}

func nilParser(_ any) error { return nil }

func TestConfigDatasources_BasicPipeline(t *testing.T) {
	res := makeResource("test-res")
	mf := &mockFormat{
		plugins: []*sdk.Plugin{},
		resources: &ResourceSources{
			Producers:    map[string]ResourceBinding{"produce.test-res.p": {Kind: "produce.test-res.p", Resource: res, Parse: nilParser}},
			Consumers:    map[string]ResourceBinding{"consume.test-res.c": {Kind: "consume.test-res.c", Resource: res, Parse: nilParser}},
			Transformers: map[string]ResourceBinding{},
		},
		pipelines: map[string]PipelineDecl{
			"my-pipeline": {
				Name:      "my-pipeline",
				Producers: []string{"produce.test-res.p"},
				Consumers: []string{"consume.test-res.c"},
			},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)
	require.NotNil(t, sources)

	// Pipelines() returns the full map
	all := sources.Pipelines()
	require.Len(t, all, 1)
	_, ok := all["my-pipeline"]
	assert.True(t, ok)

	// Pipeline(name) returns the specific pipeline
	ps, err := sources.Pipeline("my-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "my-pipeline", ps.Name)

	// Producers BindingSet yields the producer binding
	prods, err := ps.Producers(10)
	require.NoError(t, err)
	require.Len(t, prods, 1)
	assert.Equal(t, "produce.test-res.p", prods[0].Kind)
	assert.Equal(t, res, prods[0].Resource)

	// exhausted on second call
	prods, err = ps.Producers(10)
	require.NoError(t, err)
	assert.Nil(t, prods)

	// Consumers BindingSet
	cons, err := ps.Consumers(10)
	require.NoError(t, err)
	require.Len(t, cons, 1)
	assert.Equal(t, "consume.test-res.c", cons[0].Kind)
}

func TestConfigDatasources_PipelineNotFound(t *testing.T) {
	mf := &mockFormat{
		plugins:   []*sdk.Plugin{},
		resources: &ResourceSources{Producers: map[string]ResourceBinding{}, Consumers: map[string]ResourceBinding{}, Transformers: map[string]ResourceBinding{}},
		pipelines: map[string]PipelineDecl{},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)

	_, err = sources.Pipeline("missing")
	var noVal *ErrNoValue
	require.True(t, errors.As(err, &noVal))
	assert.Equal(t, "missing", noVal.Key)
}

func TestConfigDatasources_UnknownProducerRef(t *testing.T) {
	mf := &mockFormat{
		plugins:   []*sdk.Plugin{},
		resources: &ResourceSources{Producers: map[string]ResourceBinding{}, Consumers: map[string]ResourceBinding{}, Transformers: map[string]ResourceBinding{}},
		pipelines: map[string]PipelineDecl{
			"bad": {Name: "bad", Producers: []string{"produce.missing.p"}, Consumers: []string{}},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	_, err := cfg.Datasources()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "produce.missing.p")
}

func TestConfigDatasources_PluginsError(t *testing.T) {
	pluginsErr := errors.New("failed to load plugins")
	mf := &mockFormat{pluginsErr: pluginsErr}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.ErrorIs(t, err, pluginsErr)
}

func TestConfigDatasources_ResourcesError(t *testing.T) {
	resourcesErr := errors.New("failed to resolve resources")
	mf := &mockFormat{
		plugins:      []*sdk.Plugin{},
		resourcesErr: resourcesErr,
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.ErrorIs(t, err, resourcesErr)
}

func TestConfigDatasources_PipelinesError(t *testing.T) {
	pipelinesErr := errors.New("failed to parse pipelines")
	mf := &mockFormat{
		plugins:      []*sdk.Plugin{},
		resources:    &ResourceSources{Producers: map[string]ResourceBinding{}, Consumers: map[string]ResourceBinding{}, Transformers: map[string]ResourceBinding{}},
		pipelinesErr: pipelinesErr,
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	assert.Nil(t, sources)
	require.ErrorIs(t, err, pipelinesErr)
}

func TestConfigDatasources_EmptyFormat(t *testing.T) {
	mf := &mockFormat{
		plugins:   []*sdk.Plugin{},
		resources: &ResourceSources{Producers: map[string]ResourceBinding{}, Consumers: map[string]ResourceBinding{}, Transformers: map[string]ResourceBinding{}},
		pipelines: map[string]PipelineDecl{},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)
	require.NotNil(t, sources)
	assert.Empty(t, sources.Pipelines())
}

func TestConfigDatasources_PluginsPassedToResources(t *testing.T) {
	userPlugin := &sdk.Plugin{Name: "my-plugin", Resources: []*sdk.Resource{}}
	mf := &mockFormat{
		plugins:   []*sdk.Plugin{userPlugin},
		resources: &ResourceSources{Producers: map[string]ResourceBinding{}, Consumers: map[string]ResourceBinding{}, Transformers: map[string]ResourceBinding{}},
		pipelines: map[string]PipelineDecl{},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	_, err := cfg.Datasources()
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, p := range mf.capturedPlugins {
		names[p.Name] = true
	}
	assert.True(t, names["my-plugin"], "user plugin should be passed to Resources")
	assert.True(t, names["psyduck"], "stdlib plugin should be passed to Resources")
}

func TestConfigDatasources_MultipleProducers(t *testing.T) {
	res := makeResource("r")
	b1 := ResourceBinding{Kind: "produce.r.a", Resource: res, Parse: nilParser}
	b2 := ResourceBinding{Kind: "produce.r.b", Resource: res, Parse: nilParser}

	mf := &mockFormat{
		plugins: []*sdk.Plugin{},
		resources: &ResourceSources{
			Producers:    map[string]ResourceBinding{"produce.r.a": b1, "produce.r.b": b2},
			Consumers:    map[string]ResourceBinding{"consume.r.c": {Kind: "consume.r.c", Resource: res, Parse: nilParser}},
			Transformers: map[string]ResourceBinding{},
		},
		pipelines: map[string]PipelineDecl{
			"multi": {
				Name:      "multi",
				Producers: []string{"produce.r.a", "produce.r.b"},
				Consumers: []string{"consume.r.c"},
			},
		},
	}

	cfg := &Config[*mockFormat]{Format: mf}
	sources, err := cfg.Datasources()
	require.NoError(t, err)

	ps, err := sources.Pipeline("multi")
	require.NoError(t, err)

	// All producers yielded in one chunk
	prods, err := ps.Producers(10)
	require.NoError(t, err)
	require.Len(t, prods, 2)
}

func TestLiteralBindingSet(t *testing.T) {
	res := makeResource("r")
	b1 := ResourceBinding{Kind: "a", Resource: res, Parse: nilParser}
	b2 := ResourceBinding{Kind: "b", Resource: res, Parse: nilParser}
	b3 := ResourceBinding{Kind: "c", Resource: res, Parse: nilParser}

	set := LiteralBindingSet(b1, b2, b3)

	// First call: max=2, yields first two
	got, err := set(2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Kind)
	assert.Equal(t, "b", got[1].Kind)

	// Second call: max=10, yields remaining one
	got, err = set(10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "c", got[0].Kind)

	// Third call: exhausted
	got, err = set(10)
	require.NoError(t, err)
	assert.Nil(t, got)
}
