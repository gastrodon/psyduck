package datasource

import (
	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/stdlib"
)

// Format bridges a configuration language (HCL, JSON, etc.) to the
// data each Datasource needs. A Format implementation is responsible
// for parsing raw config bytes into the shapes expected here.
type Format interface {
	Plugins() ([]*sdk.Plugin, error)
	Values() (map[string]cty.Value, error)
	Resources([]*sdk.Plugin) (*ResourceSources, error)
}

// ResourceSources holds the resource data extracted by a Format,
// ready to be wrapped in Datasource accessors.
type ResourceSources struct {
	Producers    map[string]ProducerSet
	Consumers    map[string]ConsumerSet
	Transformers map[string]sdk.Transformer
}

// Sources provides access to all datasources a pipeline build needs.
type Sources interface {
	Env() Datasource[string]
	Values() Datasource[cty.Value]
	Plugins() Datasource[*sdk.Plugin]
	Producers() Datasource[ProducerSet]
	Consumers() Datasource[ConsumerSet]
	Transformers() Datasource[sdk.Transformer]
}

type sources struct {
	env          Datasource[string]
	values       Datasource[cty.Value]
	plugins      Datasource[*sdk.Plugin]
	producers    Datasource[ProducerSet]
	consumers    Datasource[ConsumerSet]
	transformers Datasource[sdk.Transformer]
}

func (s *sources) Env() Datasource[string]               { return s.env }
func (s *sources) Values() Datasource[cty.Value]         { return s.values }
func (s *sources) Plugins() Datasource[*sdk.Plugin]      { return s.plugins }
func (s *sources) Producers() Datasource[ProducerSet]    { return s.producers }
func (s *sources) Consumers() Datasource[ConsumerSet]    { return s.consumers }
func (s *sources) Transformers() Datasource[sdk.Transformer] { return s.transformers }

// Config ties a Format to the datasource construction pipeline.
type Config[F Format] struct {
	Format F
}

// Datasources drives the Format through its phases and returns
// the complete set of datasources.
func (c *Config[F]) Datasources() (Sources, error) {
	plugins, err := c.Format.Plugins()
	if err != nil {
		return nil, err
	}

	allPlugins := append(plugins, stdlib.Plugin())

	values, err := c.Format.Values()
	if err != nil {
		return nil, err
	}

	resources, err := c.Format.Resources(allPlugins)
	if err != nil {
		return nil, err
	}

	pluginMap := make(map[string]*sdk.Plugin, len(allPlugins))
	for _, p := range allPlugins {
		pluginMap[p.Name] = p
	}

	return &sources{
		env:          Env(),
		values:       &mapDatasource[cty.Value]{data: values},
		plugins:      &mapDatasource[*sdk.Plugin]{data: pluginMap},
		producers:    &mapDatasource[ProducerSet]{data: resources.Producers},
		consumers:    &mapDatasource[ConsumerSet]{data: resources.Consumers},
		transformers: &mapDatasource[sdk.Transformer]{data: resources.Transformers},
	}, nil
}
