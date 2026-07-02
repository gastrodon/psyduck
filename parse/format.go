package parse

import "github.com/psyduck-etl/sdk"

// PluginSpec identifies a plugin declared in configuration, before it has
// been fetched or loaded.
type PluginSpec struct {
	Name   string
	Source string // git URL or local path today; other schemes later
	Tag    string // optional ref to check out when fetching
}

// Format bridges a configuration language (HCL, YAML, ...) to the pipeline
// descriptions core runs. Parsing is two-phase because plugin declarations
// live in the same sources being parsed:
//
//	// init: extract specs, build the store
//	specs := format.Plugins(sources)
//	store.Build(specs)
//
//	// run: load from the store, then fully parse
//	plugins := store.Load()
//	result  := format.Parse(sources, plugins)
type Format interface {
	// Plugins extracts plugin declarations. It must not require loaded
	// plugins or evaluate anything beyond plugin declaration syntax.
	Plugins(sources []Source) ([]PluginSpec, error)

	// Parse ingests sources and resolves everything needed to build
	// pipelines. All format-specific state (eval contexts, AST nodes)
	// stays behind the returned Result and the sdk.ConfigBlocks inside it.
	Parse(sources []Source, plugins []sdk.Plugin) (Result, error)
}
