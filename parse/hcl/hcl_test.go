package hcl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// files simulates a small multi-file workspace for import tests: an
// in-memory parse.Loader backed by a map of path -> content.
type files map[string]string

func (f files) load(path string) (parse.Source, error) {
	content, ok := f[path]
	if !ok {
		return parse.Source{}, fmt.Errorf("no such file %q", path)
	}
	return parse.Source{Name: path, Content: []byte(content)}, nil
}

// src wraps a single file's content as a one-file workspace, returning the
// entry path and loader Plugins/Parse expect.
func src(content string) (string, parse.Loader) {
	return "test.psy", files{"test.psy": content}.load
}

type constantOpts struct {
	Value string `psy:"value"`
}

func testPlugin(name string) sdk.Plugin {
	return sdk.NewInProc(name,
		&sdk.Resource{
			Name:  "constant",
			Kinds: sdk.PRODUCER,
			Spec:  []*sdk.Spec{{Name: "value", Type: sdk.TypeString, Default: "0"}},
			ProvideProducer: func(p sdk.Parser) (sdk.Producer, error) {
				opts := new(constantOpts)
				if err := p(opts); err != nil {
					return nil, err
				}
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					send <- []byte(opts.Value)
					close(send)
				}, nil
			},
		},
		&sdk.Resource{
			Name:  "trash",
			Kinds: sdk.CONSUMER,
			ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) {
				return func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
					for range recv {
					}
					close(done)
				}, nil
			},
		},
		&sdk.Resource{
			Name:  "echo",
			Kinds: sdk.TRANSFORMER,
			ProvideTransformer: func(sdk.Parser) (sdk.Transformer, error) {
				return func(in []byte) ([]byte, error) { return in, nil }, nil
			},
		},
	)
}

func drainAll(t *testing.T, b parse.ResourceFunc) []parse.Resource {
	t.Helper()
	out := []parse.Resource{}
	for {
		chunk, err := b(t.Context(), 4)
		if err != nil {
			t.Fatalf("drain: %s", err)
		}
		if len(chunk) == 0 {
			return out
		}
		out = append(out, chunk...)
	}
}

func TestPlugins(t *testing.T) {
	entry, load := src(`
	plugin "amqp" {
		source = "https://github.com/psyduck-etl/amqp"
		tag    = "v1.2.3"
	}
	plugin "local" {
		source = "./plugins/local"
	}
	`)
	specs, err := NewParserHCL().Plugins(entry, load)
	if err != nil {
		t.Fatal(err)
	}

	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "amqp" || specs[0].Source != "https://github.com/psyduck-etl/amqp" || specs[0].Tag != "v1.2.3" {
		t.Fatalf("bad spec: %#v", specs[0])
	}
	if specs[1].Name != "local" || specs[1].Tag != "" {
		t.Fatalf("bad spec: %#v", specs[1])
	}
}

func TestPluginsFollowsImports(t *testing.T) {
	fs := files{
		"main.psy": `
		import {
			lib = "lib.psy"
		}
		plugin "amqp" {
			source = "https://github.com/psyduck-etl/amqp"
		}
		`,
		"lib.psy": `
		plugin "local" {
			source = "./plugins/local"
		}
		`,
	}
	specs, err := NewParserHCL().Plugins("main.psy", fs.load)
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	if !names["amqp"] || !names["local"] {
		t.Fatalf("want plugins from both main and its import, got: %#v", specs)
	}
}

func TestParse(t *testing.T) {
	t.Setenv("PSYDUCK_TEST_VALUE", "from-env")

	entry, load := src(`
	locals {
		foo = "from-value"
	}

	produce "constant" "v" {
		value      = local.foo
		stop-after = 3
		per-minute = 60
	}

	produce "test.constant" "e" {
		value = env.PSYDUCK_TEST_VALUE
	}

	consume "trash" "t" {}

	transform "echo" "x" {}

	pipeline "main" {
		produce       = [produce.constant.v, test.constant.e]
		consume       = [trash.t]
		transform     = [transform.echo.x]
		stop-after    = 9
		exit-on-error = true
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}

	pipe := result["main"]

	if pipe.StopAfter != 9 || !pipe.ExitOnError {
		t.Fatalf("bad pipeline flags: %#v", pipe)
	}

	producers := drainAll(t, pipe.Producers)
	if len(producers) != 2 {
		t.Fatalf("want 2 producers, got %d", len(producers))
	}

	first := producers[0]
	if first.Ref != "produce.constant.v" || first.Kind != sdk.PRODUCER || first.PluginID != "test" {
		t.Fatalf("bad binding: %#v", first)
	}
	if first.Meta.StopAfter != 3 || first.Meta.PerMinute != 60 {
		t.Fatalf("bad meta: %#v", first.Meta)
	}

	opts := new(constantOpts)
	if err := first.Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-value" {
		t.Fatalf("value.* not resolved: %q", opts.Value)
	}

	second := producers[1]
	if second.Ref != "produce.test.constant.e" {
		t.Fatalf("bad binding: %#v", second)
	}
	if err := second.Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-env" {
		t.Fatalf("env.* not resolved: %q", opts.Value)
	}

	if got := drainAll(t, pipe.Consumers); len(got) != 1 || got[0].Kind != sdk.CONSUMER {
		t.Fatalf("bad consumers: %#v", got)
	}
	if got := drainAll(t, pipe.Transformers); len(got) != 1 || got[0].Kind != sdk.TRANSFORMER {
		t.Fatalf("bad transformers: %#v", got)
	}
}

func TestParseAmbiguousResource(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("alpha"), testPlugin("beta")}

	entry, load := src(`
	produce "constant" "p" {}
	consume "alpha.trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err == nil || !strings.Contains(err.Error(), "alpha.constant, beta.constant") {
		t.Fatalf("want ambiguity error listing candidates, got: %v", err)
	}

	// qualification resolves the ambiguity
	entry, load = src(`
	produce "beta.constant" "p" {}
	consume "alpha.trash" "t" {}
	pipeline "main" {
		produce = [beta.constant.p]
		consume = [alpha.trash.t]
	}
	`)
	_, err = NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseDuplicates(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("test")}

	entry, load := src(`
	produce "constant" "p" {}
	produce "constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err == nil || !strings.Contains(err.Error(), "duplicate resource") {
		t.Fatalf("want duplicate resource error, got: %v", err)
	}

	entry, load = src(`
	produce "constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err = NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err == nil || !strings.Contains(err.Error(), "duplicate pipeline") {
		t.Fatalf("want duplicate pipeline error, got: %v", err)
	}
}

func TestParseProducerExclusivity(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("test")}

	entry, load := src(`
	produce "constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce      = [produce.constant.p]
		produce-from = produce.constant.p
		consume      = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want exclusivity error, got: %v", err)
	}

	entry, load = src(`
	consume "trash" "t" {}
	pipeline "main" {
		consume = [trash.t]
	}
	`)
	_, err = NewParserHCL().Parse(t.Context(), entry, load, plugins)
	if err == nil || !strings.Contains(err.Error(), "produce or produce-from is required") {
		t.Fatalf("want missing producer error, got: %v", err)
	}
}

func TestParseImportWholeFile(t *testing.T) {
	fs := files{
		"a.psy": `produce "constant" "p" { value = "hello" }`,
		"b.psy": `
		import {
			a = "a.psy"
		}
		consume "trash" "t" {}
		pipeline "main" {
			produce = [imports.a.produce.constant.p]
			consume = [trash.t]
		}
		`,
	}
	pipelines, err := NewParserHCL().Parse(t.Context(), "b.psy", fs.load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}

	got := drainAll(t, pipelines["main"].Producers)
	if len(got) != 1 {
		t.Fatalf("want 1 producer via import, got %d", len(got))
	}
	opts := new(constantOpts)
	if err := got[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "hello" {
		t.Fatalf("bad imported value: %q", opts.Value)
	}

	// b.psy's own pipeline set is exactly its own pipeline{} blocks —
	// a.psy declares none, and importing it doesn't run it.
	if len(pipelines) != 1 {
		t.Fatalf("want exactly b.psy's own pipelines, got %#v", pipelines)
	}
}

func TestParseImportReusesWholePipeline(t *testing.T) {
	fs := files{
		"a.psy": `
		produce "constant" "p" { value = "hello" }
		consume "trash" "t" {}
		pipeline "inner" {
			produce = [produce.constant.p]
			consume = [trash.t]
		}
		`,
		"b.psy": `
		import {
			a = "a.psy"
		}
		pipeline "outer" {
			produce = imports.a.pipeline.inner.produce
			consume = imports.a.pipeline.inner.consume
		}
		`,
	}
	pipelines, err := NewParserHCL().Parse(t.Context(), "b.psy", fs.load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}

	pipe, ok := pipelines["outer"]
	if !ok {
		t.Fatalf("want pipeline %q, got %#v", "outer", pipelines)
	}
	if got := drainAll(t, pipe.Producers); len(got) != 1 {
		t.Fatalf("want 1 producer reused from imported pipeline, got %d", len(got))
	}
	if got := drainAll(t, pipe.Consumers); len(got) != 1 {
		t.Fatalf("want 1 consumer reused from imported pipeline, got %d", len(got))
	}
}

func TestParseImportPluginQualified(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("alpha"), testPlugin("beta")}
	fs := files{
		"a.psy": `produce "beta.constant" "p" {}`,
		"b.psy": `
		import {
			a = "a.psy"
		}
		consume "alpha.trash" "t" {}
		pipeline "main" {
			produce = [imports.a.produce.beta.constant.p]
			consume = [alpha.trash.t]
		}
		`,
	}
	pipelines, err := NewParserHCL().Parse(t.Context(), "b.psy", fs.load, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if got := drainAll(t, pipelines["main"].Producers); len(got) != 1 {
		t.Fatalf("want 1 producer via qualified import, got %d", len(got))
	}
}

func TestParseImportCycle(t *testing.T) {
	fs := files{
		"a.psy": `import { b = "b.psy" }`,
		"b.psy": `import { a = "a.psy" }`,
	}
	_, err := NewParserHCL().Parse(t.Context(), "a.psy", fs.load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want import cycle error, got: %v", err)
	}
}

func TestParseImportDiamond(t *testing.T) {
	fs := files{
		"d.psy": `produce "constant" "p" { value = "shared" }`,
		"b.psy": `
		import { d = "d.psy" }
		consume "trash" "t" {}
		pipeline "via-b" {
			produce = [imports.d.produce.constant.p]
			consume = [trash.t]
		}
		`,
		"c.psy": `
		import { d = "d.psy" }
		consume "trash" "t" {}
		pipeline "via-c" {
			produce = [imports.d.produce.constant.p]
			consume = [trash.t]
		}
		`,
		"a.psy": `
		import {
			b = "b.psy"
			c = "c.psy"
		}
		consume "trash" "t" {}
		pipeline "main" {
			produce = [imports.b.pipeline.via-b.produce[0], imports.c.pipeline.via-c.produce[0]]
			consume = [trash.t]
		}
		`,
	}
	// b.psy and c.psy both import d.psy; d.psy should resolve once (not
	// error as a spurious duplicate or cycle), and be reachable through
	// both paths.
	pipelines, err := NewParserHCL().Parse(t.Context(), "a.psy", fs.load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatalf("diamond import should resolve cleanly, got: %v", err)
	}
	if got := drainAll(t, pipelines["main"].Producers); len(got) != 2 {
		t.Fatalf("want 2 producers reached via the diamond, got %d", len(got))
	}
}

func TestParseDuplicateLocal(t *testing.T) {
	entry, load := src(`
	locals { foo = "first" }
	locals { foo = "second" }
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, nil)
	if err == nil || !strings.Contains(err.Error(), `duplicate local "foo"`) {
		t.Fatalf("want duplicate local error, got: %v", err)
	}
}

func TestParseLocalsNotSharedAcrossImports(t *testing.T) {
	// Each file has its own locals{} namespace; two different files
	// declaring the same local key is not a conflict.
	fs := files{
		"a.psy": `locals { foo = "from-a" }`,
		"b.psy": `
		import { a = "a.psy" }
		locals { foo = "from-b" }
		produce "constant" "p" { value = local.foo }
		consume "trash" "t" {}
		pipeline "main" {
			produce = [produce.constant.p]
			consume = [trash.t]
		}
		`,
	}
	pipelines, err := NewParserHCL().Parse(t.Context(), "b.psy", fs.load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	got := drainAll(t, pipelines["main"].Producers)
	opts := new(constantOpts)
	if err := got[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-b" {
		t.Fatalf("local.* leaked across import boundary: %q", opts.Value)
	}
}

func TestParseUnknownQualifiedPlugin(t *testing.T) {
	entry, load := src(`
	produce "unknown.constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [unknown.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), `unknown plugin "unknown"`) {
		t.Fatalf("want unknown plugin error, got: %v", err)
	}
}

func TestParseUnknownResourceRef(t *testing.T) {
	// References to undeclared resources fail at HCL eval time ("Unknown variable"),
	// before resolveRefs is reached — the ref context and bindings map are always in sync.
	entry, load := src(`
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.nonexistent]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "Unknown variable") {
		t.Fatalf("want unknown variable error, got: %v", err)
	}
}

func TestParseReservedNamespaceCollision(t *testing.T) {
	// A plugin named "local" creates a short-form ref whose top-level segment
	// ("local") collides with the local.* eval namespace.
	valuePlugin := sdk.NewInProc("local",
		&sdk.Resource{
			Name:  "constant",
			Kinds: sdk.PRODUCER,
			Spec:  []*sdk.Spec{{Name: "value", Type: sdk.TypeString, Default: "0"}},
			ProvideProducer: func(p sdk.Parser) (sdk.Producer, error) {
				opts := new(constantOpts)
				if err := p(opts); err != nil {
					return nil, err
				}
				return func(_ context.Context, send chan<- []byte, errs chan<- error) { close(send) }, nil
			},
		},
	)
	entry, load := src(`
	produce "local.constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [local.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{valuePlugin, testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "collides with reserved namespace") {
		t.Fatalf("want namespace collision error, got: %v", err)
	}
}

func TestParseUnsetEnv(t *testing.T) {
	// env vars are prescanned; unset-but-queried ones resolve to ""
	entry, load := src(`
	produce "constant" "p" { value = env.PSYDUCK_DEFINITELY_UNSET_XYZ }
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	opts := new(constantOpts)
	if err := producers[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "" {
		t.Fatalf(`unset env: want "", got %q`, opts.Value)
	}

	// non-env unknown roots still error
	entry, load = src(`
	produce "constant" "p" { value = bogus.thing }
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err = NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "Unknown variable") {
		t.Fatalf("want unknown variable error, got: %v", err)
	}
}

func TestParseUnknownAttribute(t *testing.T) {
	// typo'd option names error at parse time (strict schema)
	entry, load := src(`
	produce "constant" "p" { valeu = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "Unsupported argument") {
		t.Fatalf("want unsupported argument error, got: %v", err)
	}
}

func TestParseEagerConfigError(t *testing.T) {
	// bad option values error at parse time, not at bind time
	entry, load := src(`
	produce "constant" "p" { value = ["not", "a", "string"] }
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "invalid value for value") {
		t.Fatalf("want eager conversion error, got: %v", err)
	}
}

func TestParseProduceFromEnv(t *testing.T) {
	// remote config may query env vars unseen in local sources
	t.Setenv("PSYDUCK_REMOTE_ONLY", "from-remote-env")
	seed := sdk.NewInProc("meta",
		&sdk.Resource{
			Name:  "seed",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					send <- []byte(`produce "constant" "remote" { value = env.PSYDUCK_REMOTE_ONLY }`)
					close(send)
				}, nil
			},
		},
	)

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	opts := new(constantOpts)
	if err := producers[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-remote-env" {
		t.Fatalf("remote env not resolved: %q", opts.Value)
	}
}

func TestParseProduceFrom(t *testing.T) {
	// a producer whose single message is itself psyduck config
	meta := sdk.NewInProc("meta",
		&sdk.Resource{
			Name:  "seed",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					send <- []byte(`
					produce "constant" "remote" {
						value = "from-remote"
					}
					`)
					close(send)
				}, nil
			},
		},
	)

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), meta})
	if err != nil {
		t.Fatal(err)
	}

	pipe := result["main"]

	producers := drainAll(t, pipe.Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}

	b := producers[0]
	if b.PluginID != "test" || b.Resource.Name != "constant" {
		t.Fatalf("bad remote binding: %#v", b)
	}
	if !strings.HasPrefix(b.Block.Origin().SourceName, "remote://") {
		t.Fatalf("bad remote origin: %s", b.Block.Origin())
	}

	opts := new(constantOpts)
	if err := b.Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-remote" {
		t.Fatalf("bad remote value: %q", opts.Value)
	}
}

// seedPlugin builds a produce-from seed plugin around the given producer.
func seedPlugin(p sdk.Producer) sdk.Plugin {
	return sdk.NewInProc("meta",
		&sdk.Resource{
			Name:            "seed",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) { return p, nil },
		},
	)
}

const seedEntry = `
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`

// Regression for #8: a seed that closes without sending used to read as an
// empty remote config, surfacing much later as "pipeline has no producers".
func TestParseProduceFromClosedSeed(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		close(send)
		close(errs)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	_, err = result["main"].Producers(t.Context(), 4)
	if err == nil || !strings.Contains(err.Error(), "closed without sending") {
		t.Fatalf("want closed-without-sending error, got %v", err)
	}
}

// Draining a produce-from stream is bounded by the caller's ctx, not only
// by the builtin timeout. The seed honors ctx like a well-behaved plugin
// should — it's the stream's own ctx.Done() handling under test here, not
// resilience against a non-cooperating plugin (that's core's job, see
// core/regression_test.go).
func TestParseProduceFromCancel(t *testing.T) {
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		<-ctx.Done() // never sends
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = result["main"].Producers(ctx, 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

func TestParseProduceFromStream(t *testing.T) {
	// a seed that emits multiple messages, each defining new produce
	// blocks. Every message should surface on the Producers stream.
	values := []string{"one", "two", "three"}
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		for _, v := range values {
			send <- fmt.Appendf(nil, `produce "constant" "remote" { value = %q }`, v)
		}
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != len(values) {
		t.Fatalf("want %d streamed remote producers, got %d", len(values), len(producers))
	}
	for i, b := range producers {
		opts := new(constantOpts)
		if err := b.Block.Decode(opts); err != nil {
			t.Fatalf("producer %d decode: %s", i, err)
		}
		if opts.Value != values[i] {
			t.Fatalf("producer %d value: got %q, want %q", i, opts.Value, values[i])
		}
	}
}

// Messages that declare no producers are skipped, not treated as the
// stream's first delivery — the bindings from a later message still arrive.
func TestParseProduceFromEmptyMessage(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		send <- []byte(`# nothing declared`)
		send <- []byte(`produce "constant" "remote" { value = "real" }`)
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}
}

// Calling the stream with max < 1 releases it: the seed producer is
// stopped and later drains observe exhaustion.
func TestParseProduceFromRelease(t *testing.T) {
	stopped := make(chan struct{})
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		select {
		case send <- []byte(`produce "constant" "remote" { value = "x" }`):
		case <-ctx.Done():
			close(stopped)
			return
		}
		<-ctx.Done()
		close(stopped)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	stream := result["main"].Producers
	chunk, err := stream(t.Context(), 4)
	if err != nil || len(chunk) != 1 {
		t.Fatalf("first drain: got %d resources, err %v", len(chunk), err)
	}

	if _, err := stream(t.Context(), 0); err != nil {
		t.Fatalf("release: %s", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("seed producer not stopped by release")
	}

	chunk, err = stream(t.Context(), 4)
	if err != nil || chunk != nil {
		t.Fatalf("drain after release: got %v, err %v; want exhaustion", chunk, err)
	}
}

func TestParseProduceFromParallel(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce              = [produce.constant.p]
		consume              = [trash.t]
		produce-from-parallel = 3
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["main"].ProduceFromParallel; got != 3 {
		t.Fatalf("ProduceFromParallel: got %d, want 3", got)
	}
}

func TestParseProduceFromParallelNegative(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce              = [produce.constant.p]
		consume              = [trash.t]
		produce-from-parallel = -1
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "produce-from-parallel") {
		t.Fatalf("want produce-from-parallel error, got: %v", err)
	}
}

func TestParseProduceFromTimeout(t *testing.T) {
	// a seed that never sends should trip the configured
	// produce-from-timeout, rather than the 10-second default.
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		<-ctx.Done() // never sends
	})

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 1
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	_, err = result["main"].Producers(t.Context(), 4)
	if err == nil || !strings.Contains(err.Error(), "timeout waiting for remote producer") {
		t.Fatalf("want timeout error, got: %v", err)
	}
}

func TestParseProduceFromTimeoutZero(t *testing.T) {
	// produce-from-timeout = 0 disables the first-message timeout entirely.
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		time.Sleep(1200 * time.Millisecond)
		send <- []byte(`produce "constant" "remote" { value = "late" }`)
		close(send)
	})

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 0
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}
}

func TestParseProduceFromTimeoutNegative(t *testing.T) {
	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = -1
	}
	`)
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) { close(send) })
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err == nil || !strings.Contains(err.Error(), "produce-from-timeout") {
		t.Fatalf("want produce-from-timeout error, got: %v", err)
	}
}
