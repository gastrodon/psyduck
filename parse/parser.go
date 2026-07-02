package parse

import "github.com/psyduck-etl/sdk"

// Plugin identifies a plugin declared in configuration, before it has
// been fetched or loaded.
type Plugin struct {
	Name   string
	Source string // git URL or local path today; other schemes later
	Tag    string // optional ref to check out when fetching
}

// Parser bridges a configuration language (HCL, YAML, ...) to the pipeline
// descriptions core runs. Parsing is two-phase because plugin declarations
// live in the same sources being parsed:
//
//	// init: extract specs, build the store
//	specs := parser.Plugins(sources)
//	store.Build(specs)
//
//	// run: load from the store, then fully parse
//	plugins := store.Load()
//	pipelines, err := parser.Parse(sources, plugins)
type Parser interface {
	// Plugins extracts plugin declarations. It must not require loaded
	// plugins or evaluate anything beyond plugin declaration syntax.
	Plugins(sources []Source) ([]Plugin, error)

	// Parse ingests sources and resolves everything needed to build
	// pipelines. All parser-specific state (eval contexts, AST nodes)
	// stays behind the Pipelines and the sdk.ConfigBlocks inside them.
	Parse(sources []Source, plugins []sdk.Plugin) (map[string]Pipeline, error)
}
