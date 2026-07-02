package hcl

import (
	"strings"
	"testing"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

func srcs(content string) []parse.Source {
	return []parse.Source{{Name: "test.psy", Content: []byte(content)}}
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
				return func(send chan<- []byte, errs chan<- error) {
					send <- []byte(opts.Value)
					close(send)
				}, nil
			},
		},
		&sdk.Resource{
			Name:  "trash",
			Kinds: sdk.CONSUMER,
			ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) {
				return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
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
		chunk, err := b(4)
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
	specs, err := NewParserHCL().Plugins(srcs(`
	plugin "amqp" {
		source = "https://github.com/psyduck-etl/amqp"
		tag    = "v1.2.3"
	}
	plugin "local" {
		source = "./plugins/local"
	}
	`))
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

func TestParse(t *testing.T) {
	t.Setenv("PSYDUCK_TEST_VALUE", "from-env")

	result, err := NewParserHCL().Parse(srcs(`
	value {
		foo = "from-value"
	}

	produce "constant" "v" {
		value      = value.foo
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
	`), []sdk.Plugin{testPlugin("test")})
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

	_, err := NewParserHCL().Parse(srcs(`
	produce "constant" "p" {}
	consume "alpha.trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`), plugins)
	if err == nil || !strings.Contains(err.Error(), "alpha.constant, beta.constant") {
		t.Fatalf("want ambiguity error listing candidates, got: %v", err)
	}

	// qualification resolves the ambiguity
	_, err = NewParserHCL().Parse(srcs(`
	produce "beta.constant" "p" {}
	consume "alpha.trash" "t" {}
	pipeline "main" {
		produce = [beta.constant.p]
		consume = [alpha.trash.t]
	}
	`), plugins)
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseDuplicates(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("test")}

	_, err := NewParserHCL().Parse(srcs(`
	produce "constant" "p" {}
	produce "constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.p]
		consume = [trash.t]
	}
	`), plugins)
	if err == nil || !strings.Contains(err.Error(), "duplicate resource") {
		t.Fatalf("want duplicate resource error, got: %v", err)
	}

	_, err = NewParserHCL().Parse(srcs(`
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
	`), plugins)
	if err == nil || !strings.Contains(err.Error(), "duplicate pipeline") {
		t.Fatalf("want duplicate pipeline error, got: %v", err)
	}
}

func TestParseProducerExclusivity(t *testing.T) {
	plugins := []sdk.Plugin{testPlugin("test")}

	_, err := NewParserHCL().Parse(srcs(`
	produce "constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce      = [produce.constant.p]
		produce-from = produce.constant.p
		consume      = [trash.t]
	}
	`), plugins)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want exclusivity error, got: %v", err)
	}

	_, err = NewParserHCL().Parse(srcs(`
	consume "trash" "t" {}
	pipeline "main" {
		consume = [trash.t]
	}
	`), plugins)
	if err == nil || !strings.Contains(err.Error(), "produce or produce-from is required") {
		t.Fatalf("want missing producer error, got: %v", err)
	}
}

func TestParseMultiSource(t *testing.T) {
	sources := []parse.Source{
		{Name: "a.psy", Content: []byte(`
		produce "constant" "p" { value = "hello" }
		`)},
		{Name: "b.psy", Content: []byte(`
		consume "trash" "t" {}
		pipeline "main" {
			produce = [produce.constant.p]
			consume = [trash.t]
		}
		`)},
	}
	pipelines, err := NewParserHCL().Parse(sources, []sdk.Plugin{testPlugin("test")})
	if err != nil {
		t.Fatal(err)
	}
	if got := drainAll(t, pipelines["main"].Producers); len(got) != 1 {
		t.Fatalf("want 1 producer across sources, got %d", len(got))
	}
}

func TestParseDuplicateValueKeyAcrossSources(t *testing.T) {
	sources := []parse.Source{
		{Name: "a.psy", Content: []byte(`value { foo = "first" }`)},
		{Name: "b.psy", Content: []byte(`value { foo = "second" }`)},
	}
	_, err := NewParserHCL().Parse(sources, nil)
	if err == nil || !strings.Contains(err.Error(), `duplicate value key "foo"`) {
		t.Fatalf("want duplicate value key error, got: %v", err)
	}
}

func TestParseUnknownQualifiedPlugin(t *testing.T) {
	_, err := NewParserHCL().Parse(srcs(`
	produce "unknown.constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [unknown.constant.p]
		consume = [trash.t]
	}
	`), []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), `unknown plugin "unknown"`) {
		t.Fatalf("want unknown plugin error, got: %v", err)
	}
}

func TestParseUnknownResourceRef(t *testing.T) {
	// References to undeclared resources fail at HCL eval time ("Unknown variable"),
	// before resolveRefs is reached — the ref context and bindings map are always in sync.
	_, err := NewParserHCL().Parse(srcs(`
	consume "trash" "t" {}
	pipeline "main" {
		produce = [produce.constant.nonexistent]
		consume = [trash.t]
	}
	`), []sdk.Plugin{testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "Unknown variable") {
		t.Fatalf("want unknown variable error, got: %v", err)
	}
}

func TestParseReservedNamespaceCollision(t *testing.T) {
	// A plugin named "value" creates a short-form ref whose top-level segment
	// ("value") collides with the value.* eval namespace.
	valuePlugin := sdk.NewInProc("value",
		&sdk.Resource{
			Name:  "constant",
			Kinds: sdk.PRODUCER,
			Spec:  []*sdk.Spec{{Name: "value", Type: sdk.TypeString, Default: "0"}},
			ProvideProducer: func(p sdk.Parser) (sdk.Producer, error) {
				opts := new(constantOpts)
				if err := p(opts); err != nil {
					return nil, err
				}
				return func(send chan<- []byte, errs chan<- error) { close(send) }, nil
			},
		},
	)
	_, err := NewParserHCL().Parse(srcs(`
	produce "value.constant" "p" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce = [value.constant.p]
		consume = [trash.t]
	}
	`), []sdk.Plugin{valuePlugin, testPlugin("test")})
	if err == nil || !strings.Contains(err.Error(), "collides with reserved namespace") {
		t.Fatalf("want namespace collision error, got: %v", err)
	}
}

func TestParseProduceFrom(t *testing.T) {
	// a producer whose single message is itself psyduck config
	meta := sdk.NewInProc("meta",
		&sdk.Resource{
			Name:  "seed",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
				return func(send chan<- []byte, errs chan<- error) {
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

	result, err := NewParserHCL().Parse(srcs(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`), []sdk.Plugin{testPlugin("test"), meta})
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
