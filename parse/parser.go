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
// descriptions core runs. Parsing operates on one entry file at a time;
// import{} declarations inside it (and transitively inside whatever it
// imports) are followed on demand via load. Parsing is two-phase because
// plugin declarations live in the same sources being parsed:
//
//	// init: extract specs (following imports), build the store
//	specs := parser.Plugins(entry, parse.OSLoader)
//	store.Build(specs)
//
//	// run: load from the store, then fully parse
//	plugins := store.Load()
//	pipelines, err := parser.Parse(entry, parse.OSLoader, plugins)
type Parser interface {
	// Plugins extracts every plugin{} declaration reachable from entry,
	// following import{} blocks transitively. It must not require loaded
	// plugins or evaluate anything beyond plugin/import declaration
	// syntax.
	Plugins(entry string, load Loader) ([]Plugin, error)

	// Parse resolves entry — and its transitive imports — against the
	// given plugins, and returns every pipeline{} block declared directly
	// in entry (pipelines reachable only via import don't run on their
	// own; they're data for entry's pipelines to reuse). All
	// parser-specific state (eval contexts, AST nodes) stays behind the
	// Pipelines and the sdk.ConfigBlocks inside them.
	Parse(entry string, load Loader, plugins []sdk.Plugin) (map[string]Pipeline, error)
}
