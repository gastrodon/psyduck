package hcl

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
			ProvideProducer: func(_ context.Context, p sdk.Parser) (sdk.Producer, error) {
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
			ProvideConsumer: func(_ context.Context, _ sdk.Parser) (sdk.Consumer, error) {
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
			ProvideTransformer: func(_ context.Context, _ sdk.Parser) (sdk.Transformer, error) {
				return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
					defer close(out)
					for {
						select {
						case msg, ok := <-in:
							if !ok {
								return
							}
							select {
							case out <- msg:
							case <-ctx.Done():
								return
							}
						case <-ctx.Done():
							return
						}
					}
				}, nil
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
		exit-on-error = true
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}

	pipe := result["main"]

	if !pipe.ExitOnError {
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

func TestParseStopAfterProducerOnly(t *testing.T) {
	// stop-after is a producer-only flow governor: consume, transform, and
	// pipeline blocks all reject it as an unknown attribute.
	cases := []struct {
		name string
		body string
	}{
		{"consume", `
		produce "constant" "p" {}
		consume "trash" "t" { stop-after = 3 }
		pipeline "main" {
			produce = [produce.constant.p]
			consume = [trash.t]
		}
		`},
		{"transform", `
		produce "constant" "p" {}
		consume "trash" "t" {}
		transform "echo" "x" { stop-after = 3 }
		pipeline "main" {
			produce   = [produce.constant.p]
			consume   = [trash.t]
			transform = [transform.echo.x]
		}
		`},
		{"transform-per-minute", `
		produce "constant" "p" {}
		consume "trash" "t" {}
		transform "echo" "x" { per-minute = 60 }
		pipeline "main" {
			produce   = [produce.constant.p]
			consume   = [trash.t]
			transform = [transform.echo.x]
		}
		`},
		{"pipeline", `
		produce "constant" "p" {}
		consume "trash" "t" {}
		pipeline "main" {
			produce    = [produce.constant.p]
			consume    = [trash.t]
			stop-after = 3
		}
		`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry, load := src(c.body)
			_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
			if err == nil || !strings.Contains(err.Error(), "Unsupported argument") {
				t.Fatalf("want unsupported argument error, got: %v", err)
			}
		})
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
			ProvideProducer: func(_ context.Context, p sdk.Parser) (sdk.Producer, error) {
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

func TestParseProduceParallel(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce          = [produce.constant.p]
		consume          = [trash.t]
		produce-parallel = 3
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["main"].ProduceParallel; got != 3 {
		t.Fatalf("ProduceParallel: got %d, want 3", got)
	}
}

// Absent, produce-parallel defaults to 1: producers run one at a time unless
// the pipeline opts into more.
func TestParseProduceParallelAbsent(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
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
	if got := result["main"].ProduceParallel; got != 1 {
		t.Fatalf("ProduceParallel default: got %d, want 1", got)
	}
}

func TestParseProduceParallelNegative(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce          = [produce.constant.p]
		consume          = [trash.t]
		produce-parallel = -1
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("want produce-parallel floor error, got: %v", err)
	}
}

// LIVE BUG (discovered by QA audit — this test FAILS at the commit that
// introduces it): a fractional produce-parallel must be rejected at parse,
// not silently truncated. Today resource.go takes AsBigFloat().Int64() and
// discards the fraction, so produce-parallel = 2.9 quietly becomes 2 — the
// strict-schema philosophy the rest of the parser follows (typos and bad
// value types error at parse time) says this should be an error instead.
func TestParseProduceParallelFractional(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" {}
	pipeline "main" {
		produce          = [produce.constant.p]
		consume          = [trash.t]
		produce-parallel = 2.9
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "produce-parallel") {
		t.Fatalf("want produce-parallel fractional rejection, got: %v", err)
	}
}

// parallel = n on a resource block stamps out n literal copies of it into
// the pipeline list — one producer written parallel = 3 drains as three.
func TestParseParallelDuplicatesProducers(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" {
		value    = "x"
		parallel = 3
	}
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
	if len(producers) != 3 {
		t.Fatalf("want 3 producers from parallel = 3, got %d", len(producers))
	}
	for i, p := range producers {
		if p.Ref != "produce.constant.p" {
			t.Fatalf("copy %d: bad ref %q", i, p.Ref)
		}
	}
	if got := result["main"].Spec.Producers; len(got) != 3 {
		t.Fatalf("Spec.Producers: want 3, got %d", len(got))
	}
}

// parallel works the same on consumers and transformers — the parser expands
// their literal lists before core ever sees them.
func TestParseParallelDuplicatesConsumersAndTransformers(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
	consume "trash" "t" { parallel = 2 }
	transform "echo" "x" { parallel = 4 }
	pipeline "main" {
		produce   = [produce.constant.p]
		transform = [transform.echo.x]
		consume   = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := drainAll(t, result["main"].Consumers); len(got) != 2 {
		t.Fatalf("want 2 consumers from parallel = 2, got %d", len(got))
	}
	if got := drainAll(t, result["main"].Transformers); len(got) != 4 {
		t.Fatalf("want 4 transformers from parallel = 4, got %d", len(got))
	}
}

// Absent, parallel defaults to 1: one copy, exactly as before the field
// existed.
func TestParseParallelAbsent(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" { value = "x" }
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
	if got := drainAll(t, result["main"].Producers); len(got) != 1 {
		t.Fatalf("default parallel: want 1 producer, got %d", len(got))
	}
}

// parallel is a duplication count, so 0 is meaningless and rejected — mirrors
// the "must be >= 1" contract the field promises.
func TestParseParallelZeroRejected(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" {
		value    = "x"
		parallel = 0
	}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "parallel: must be >= 1") {
		t.Fatalf("want parallel >= 1 rejection, got: %v", err)
	}
}

func TestParseParallelNegativeRejected(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" {
		value    = "x"
		parallel = -2
	}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "parallel: must be >= 1") {
		t.Fatalf("want parallel >= 1 rejection, got: %v", err)
	}
}

// A fractional parallel is rejected rather than truncated, following the
// strict-schema philosophy the rest of the parser holds to.
func TestParseParallelFractionalRejected(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" {
		value    = "x"
		parallel = 2.5
	}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "parallel: must be a whole number") {
		t.Fatalf("want fractional parallel rejection, got: %v", err)
	}
}

// parallel on a produce-from seed is rejected: a seed is one live stream, not
// something duplication has a sensible meaning for.
func TestParseParallelProduceFromSeedRejected(t *testing.T) {
	entry, load := src(`
	produce "constant" "seed" {
		value    = "x"
		parallel = 2
	}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.constant.seed
		consume      = [trash.t]
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "parallel is not supported on a produce-from seed") {
		t.Fatalf("want produce-from seed rejection, got: %v", err)
	}
}

// parallel and produce-parallel compose: expansion happens first, so
// produce-parallel = 0 ("all at once") counts the duplicated producers.
func TestParseParallelWithProduceParallelZero(t *testing.T) {
	entry, load := src(`
	produce "constant" "p" {
		value    = "x"
		parallel = 4
	}
	consume "trash" "t" {}
	pipeline "main" {
		produce          = [produce.constant.p]
		consume          = [trash.t]
		produce-parallel = 0
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["main"].ProduceParallel; got != 4 {
		t.Fatalf("ProduceParallel: want 4 (one per expanded producer), got %d", got)
	}
}

// With a static produce list, produce-parallel = 0 means "run them all at
// once" — it resolves to the number of producers declared.
func TestParseProduceParallelZeroStatic(t *testing.T) {
	entry, load := src(`
	produce "constant" "a" { value = "x" }
	produce "constant" "b" { value = "y" }
	produce "constant" "c" { value = "z" }
	consume "trash" "t" {}
	pipeline "main" {
		produce          = [produce.constant.a, produce.constant.b, produce.constant.c]
		consume          = [trash.t]
		produce-parallel = 0
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["main"].ProduceParallel; got != 3 {
		t.Fatalf("ProduceParallel: got %d, want 3 (one per producer)", got)
	}
}
