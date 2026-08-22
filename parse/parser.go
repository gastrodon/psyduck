package parse

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

// BuildMode selects the toolchain fetch uses to turn a plugin's source
// into an executable. It's an enum, not a general plugin-toolchain
// system: exactly the values psyduck's own fetch/build dispatch knows
// how to run.
type BuildMode string

const (
	// BuildModeGo is the default: `go build -C <codePath> -o <out>`.
	BuildModeGo BuildMode = "go"
	// BuildModeBun runs the bun-equivalent build, mirroring a bun plugin's
	// own `bun build --compile ...` build script.
	BuildModeBun BuildMode = "bun"
)

// Plugin identifies a plugin declared in configuration, before it has
// been fetched or loaded.
type Plugin struct {
	Name      string
	Source    string    // git URL or local path today; other schemes later
	Tag       string    // optional ref to check out when fetching
	BuildMode BuildMode // how to build Source into an executable; "" means BuildModeGo
}

// Parser bridges a configuration language (HCL, YAML, ...) to the pipeline
// descriptions core runs. Parsing operates on one entry file at a time;
// import{} declarations inside it (and transitively inside whatever it
// imports) are followed on demand via load. Parsing is two-phase because
// plugin declarations live in the same sources being parsed:
//
//	// init: extract specs (following imports), build the store, lock it
//	specs := parser.Plugins(entry, parse.FileLoader)
//	locked, err := store.Build(specs)
//	plugins.WriteLock(entry, &plugins.Lock{Plugins: locked})
//
//	// run: read the lock, load from the store, then fully parse
//	lock, err := plugins.ReadLock(entry)
//	loaded, err := store.Load(lock.Plugins)
//	pipelines, err := parser.Parse(entry, parse.FileLoader, loaded)
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
	//
	// ctx bounds parse-time work only. Deferred work carried by the
	// returned Pipelines (a produce-from seed runs when its ResourceFunc
	// is drained) is bounded by the ctx handed to that drain instead.
	Parse(ctx context.Context, entry string, load Loader, plugins []sdk.Plugin) (map[string]Pipeline, error)
}
