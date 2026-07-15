package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// nopBlock is a stand-in sdk.ConfigBlock for resources with no options.
type nopBlock struct{}

func (nopBlock) Origin() sdk.SourceRange { return sdk.SourceRange{SourceName: "test"} }
func (nopBlock) Decode(any) error        { return nil }
func (nopBlock) Encode() ([]byte, error) { return []byte("{}"), nil }

// corePlugin returns an in-proc plugin with one resource of each kind:
// a producer emitting `count` copies of `payload`, a counting consumer,
// and a transformer that appends `suffix` to each message.
func corePlugin(name string, payload []byte, count int, consumed *int, suffix string) sdk.Plugin {
	return sdk.NewInProc(name,
		&sdk.Resource{
			Name:  "emit",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					for i := 0; i < count; i++ {
						send <- payload
					}
					close(send)
				}, nil
			},
		},
		&sdk.Resource{
			Name:  "count",
			Kinds: sdk.CONSUMER,
			ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) {
				return func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
					for range recv {
						*consumed++
					}
					close(done)
				}, nil
			},
		},
		&sdk.Resource{
			Name:  "suffix",
			Kinds: sdk.TRANSFORMER,
			ProvideTransformer: func(sdk.Parser) (sdk.Transformer, error) {
				return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
					defer close(out)
					for {
						select {
						case msg, ok := <-in:
							if !ok {
								return
							}
							select {
							case out <- append(msg, suffix...):
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

func testResource(pluginID, resource string, kind sdk.Kind, meta sdk.BlockMeta) parse.Resource {
	return parse.Resource{
		Ref:      fmt.Sprintf("%s.%s.t", pluginID, resource),
		Kind:     kind,
		Resource: sdk.ResourceDescriptor{Name: resource},
		PluginID: pluginID,
		Block:    nopBlock{},
		Meta:     meta,
	}
}

// runComposed feeds inputs through tx synchronously and collects whatever it
// emits on out and errs, closing in and waiting for out to close. A hang is a
// test failure, not a timeout to tolerate.
func runComposed(t *testing.T, tx sdk.Transformer, inputs ...[]byte) ([][]byte, []error) {
	t.Helper()
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, len(inputs)+1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		tx(context.Background(), in, out, errs)
	}()
	go func() {
		defer close(in)
		for _, msg := range inputs {
			in <- msg
		}
	}()

	var got [][]byte
	for msg := range out {
		got = append(got, msg)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("composed transformer did not finish: hung after closing out")
	}
	close(errs)
	var errsGot []error
	for e := range errs {
		errsGot = append(errsGot, e)
	}
	return got, errsGot
}

func Test_composeTransformers(t *testing.T) {
	appendc := func(c byte) sdk.Transformer {
		return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
			defer close(out)
			for {
				select {
				case msg, ok := <-in:
					if !ok {
						return
					}
					select {
					case out <- append(msg, c):
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}

	// empty stack composes to nil, the "no transform stage" bypass signal
	if composeTransformers(nil) != nil {
		t.Fatal("empty stack: want nil")
	}

	// single transformer passes through unwrapped
	got, _ := runComposed(t, composeTransformers([]sdk.Transformer{appendc('a')}), []byte("_"))
	if len(got) != 1 || string(got[0]) != "_a" {
		t.Fatalf("single: %v", got)
	}

	// applied in declaration order
	got, _ = runComposed(t, composeTransformers([]sdk.Transformer{appendc('a'), appendc('b'), appendc('c')}), []byte("_"))
	if len(got) != 1 || string(got[0]) != "_abc" {
		t.Fatalf("order: %v", got)
	}

	// a stage's error is reported and that message drops, but the chain keeps running
	boom := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for range in {
			select {
			case errs <- fmt.Errorf("boom"):
			case <-ctx.Done():
				return
			}
		}
	}
	got, errsGot := runComposed(t, composeTransformers([]sdk.Transformer{appendc('a'), boom, appendc('c')}), []byte("_"))
	if len(got) != 0 {
		t.Fatalf("boom: want no output, got %v", got)
	}
	if len(errsGot) != 1 || errsGot[0].Error() != "boom" {
		t.Fatalf("boom: want [boom], got %v", errsGot)
	}

	// a filtering stage (emits nothing) short-circuits later transformers
	filter := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for range in {
		}
	}
	called := false
	spy := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for range in {
			called = true
		}
	}
	got, _ = runComposed(t, composeTransformers([]sdk.Transformer{filter, spy}), []byte("_"))
	if len(got) != 0 || called {
		t.Fatalf("filter: out=%v called=%v", got, called)
	}
}

// Test_composeTransformers_ContinuesAfterError proves the chain keeps
// processing later messages after a middle stage drops one to an error — the
// Test_composeTransformers boom case only feeds a single message, so it shows
// the drop but not that the stream survives it. appendByte is reused from
// run_test.go (same package).
func Test_composeTransformers_ContinuesAfterError(t *testing.T) {
	// errors on the middle stage's view of message "2" ("2a" after the first
	// stage), passing everything else straight through.
	selErr := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for msg := range in {
			if string(msg) == "2a" {
				select {
				case errs <- fmt.Errorf("boom on %s", msg):
				case <-ctx.Done():
					return
				}
				continue
			}
			select {
			case out <- msg:
			case <-ctx.Done():
				return
			}
		}
	}

	chain := composeTransformers([]sdk.Transformer{appendByte('a'), selErr, appendByte('c')})
	got, errsGot := runComposed(t, chain, []byte("1"), []byte("2"), []byte("3"))

	if len(got) != 2 || string(got[0]) != "1ac" || string(got[1]) != "3ac" {
		t.Fatalf("want [1ac 3ac] to survive the error, got %s", got)
	}
	if len(errsGot) != 1 || errsGot[0].Error() != "boom on 2a" {
		t.Fatalf("want one boom error, got %v", errsGot)
	}
}

func Test_drain(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("m"), 2, &consumed, "!")
	plugins := map[string]sdk.Plugin{"p": plugin}

	// chunking: more resources than bindChunk, all collected in order
	n := bindChunk*2 + 3
	resources := make([]parse.Resource, n)
	for i := range resources {
		r := testResource("p", "suffix", sdk.TRANSFORMER, sdk.BlockMeta{})
		r.Ref = fmt.Sprintf("transform.suffix.t%d", i)
		resources[i] = r
	}
	seen := []string{}
	err := drain(t.Context(), parse.LiteralResourceFunc(resources...), plugins, func(b parse.Resource, _ sdk.Instance) {
		seen = append(seen, b.Ref)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != n || seen[0] != "transform.suffix.t0" || seen[n-1] != fmt.Sprintf("transform.suffix.t%d", n-1) {
		t.Fatalf("bad drain order/count: %d %v", len(seen), seen)
	}

	// unknown plugin
	err = drain(t.Context(), parse.LiteralResourceFunc(testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{})), plugins, func(parse.Resource, sdk.Instance) {})
	if err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want no-plugin error, got %v", err)
	}

	// bind failure (unknown resource within the plugin)
	err = drain(t.Context(), parse.LiteralResourceFunc(testResource("p", "nonexistent", sdk.PRODUCER, sdk.BlockMeta{})), plugins, func(parse.Resource, sdk.Instance) {})
	if err == nil {
		t.Fatal("want bind error, got nil")
	}

	// stream error propagates
	streamErr := func(context.Context, int) ([]parse.Resource, error) { return nil, fmt.Errorf("stream broke") }
	if err := drain(t.Context(), streamErr, plugins, func(parse.Resource, sdk.Instance) {}); err == nil || err.Error() != "stream broke" {
		t.Fatalf("want stream error, got %v", err)
	}
}

func Test_BuildPipeline(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("msg"), 5, &consumed, "!")

	// ResourceFunc streams are one-shot; build a fresh description per run
	mksrc := func(producerMeta sdk.BlockMeta) parse.Pipeline {
		return parse.Pipeline{
			Name:            "main",
			Producers:       parse.LiteralResourceFunc(testResource("p", "emit", sdk.PRODUCER, producerMeta)),
			Consumers:       parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
			Transformers:    parse.LiteralResourceFunc(testResource("p", "suffix", sdk.TRANSFORMER, sdk.BlockMeta{})),
			ExitOnError:     true,
			ProduceParallel: 1,
		}
	}

	src := mksrc(sdk.BlockMeta{})
	pipeline, err := BuildPipeline(t.Context(), src, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if !pipeline.ExitOnError {
		t.Fatalf("pipeline flags not propagated: %#v", pipeline)
	}
	if err := RunPipeline(t.Context(), pipeline); err != nil {
		t.Fatal(err)
	}
	if consumed != 5 {
		t.Fatalf("want 5 consumed, got %d", consumed)
	}

	// producer meta applies: stop-after 2 of 5
	consumed = 0
	pipeline, err = BuildPipeline(t.Context(), mksrc(sdk.BlockMeta{StopAfter: 2}), []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunPipeline(t.Context(), pipeline); err != nil {
		t.Fatal(err)
	}
	if consumed != 2 {
		t.Fatalf("want 2 consumed with StopAfter, got %d", consumed)
	}
}

func Test_BuildPipeline_errors(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("m"), 1, &consumed, "")
	empty := parse.LiteralResourceFunc()

	// Consumers drain eagerly at build; LiteralResourceFunc is one-shot, so a
	// fresh one is needed per BuildPipeline call.
	mkConsumer := func() parse.ResourceFunc {
		return parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{}))
	}
	producer := parse.LiteralResourceFunc(testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{}))

	// No consumers is still a build-time failure.
	_, err := BuildPipeline(t.Context(), parse.Pipeline{Name: "x", Producers: producer, Consumers: empty, Transformers: empty}, []sdk.Plugin{plugin})
	if err == nil || !strings.Contains(err.Error(), "no consumers") {
		t.Fatalf("want no-consumers error, got %v", err)
	}

	// Producers bind lazily now: an empty producer stream builds fine and the
	// run finishes normally, having delivered nothing.
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{Name: "x", Producers: empty, Consumers: mkConsumer(), Transformers: empty}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatalf("empty producers should build, got %v", err)
	}
	if err := RunPipeline(t.Context(), pipeline); err != nil {
		t.Fatalf("empty producers should run to a clean finish, got %v", err)
	}

	// An unknown producer plugin also surfaces at run time, not build.
	ghost := parse.LiteralResourceFunc(testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{}))
	pipeline, err = BuildPipeline(t.Context(), parse.Pipeline{Name: "x", Producers: ghost, Consumers: mkConsumer(), Transformers: empty, ExitOnError: true}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatalf("ghost producer should build, got %v", err)
	}
	if err := RunPipeline(t.Context(), pipeline); err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want no-plugin error at run, got %v", err)
	}
}

// closeTracker is an sdk.Plugin whose instances record Close calls. A bound
// instance is the engine's handle on plugin-side resources: for subprocess
// plugins each one is an entry in the plugin server's handle table that only
// a Close RPC frees, so an engine that never closes what it binds grows the
// subprocess for the life of the run (most visibly under produce-from, which
// keeps binding fresh producers).
type closeTracker struct {
	mu    sync.Mutex // producers bind at run time, so Bind may be concurrent
	bound []*trackedInstance

	produce   sdk.Producer
	consume   sdk.Consumer
	transform sdk.Transformer
}

func (p *closeTracker) Name() string                        { return "closetrack" }
func (p *closeTracker) Resources() []sdk.ResourceDescriptor { return nil }

func (p *closeTracker) Bind(kind sdk.Kind, _ string, _ sdk.ConfigBlock) (sdk.Instance, error) {
	inst := &trackedInstance{kind: kind, plugin: p}
	p.mu.Lock()
	p.bound = append(p.bound, inst)
	p.mu.Unlock()
	return inst, nil
}

type trackedInstance struct {
	kind   sdk.Kind
	plugin *closeTracker
	closes atomic.Int64
}

func (i *trackedInstance) Kind() sdk.Kind { return i.kind }

func (i *trackedInstance) Produce(ctx context.Context, send chan<- []byte, errs chan<- error) {
	i.plugin.produce(ctx, send, errs)
}

func (i *trackedInstance) Consume(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
	i.plugin.consume(ctx, recv, errs, done)
}

func (i *trackedInstance) Transform(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
	i.plugin.transform(ctx, in, out, errs)
}

func (i *trackedInstance) Close() error {
	i.closes.Add(1)
	return nil
}

// Test_RunPipeline_ClosesBoundInstances pins the instance lifecycle contract:
// every instance the engine binds — eagerly (consumers, transformers) or
// lazily at run time (producers) — is Closed by the time RunPipeline returns.
// Close is how a plugin releases whatever Bind acquired (connections, files,
// and for gRPC subprocess plugins the server-side handle itself), so a leak
// here grows plugin subprocesses for the life of the run.
func Test_RunPipeline_ClosesBoundInstances(t *testing.T) {
	const n = 100
	var got atomic.Int64
	plugin := &closeTracker{
		produce: emitN(n, []byte("x"), nil),
		consume: countAll(&got),
		transform: func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
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
		},
	}

	src := parse.Pipeline{
		Name:            "close-track",
		Producers:       parse.LiteralResourceFunc(testResource("closetrack", "emit", sdk.PRODUCER, sdk.BlockMeta{})),
		Consumers:       parse.LiteralResourceFunc(testResource("closetrack", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:    parse.LiteralResourceFunc(testResource("closetrack", "pass", sdk.TRANSFORMER, sdk.BlockMeta{})),
		ProduceParallel: 1,
	}

	pipeline, err := BuildPipeline(t.Context(), src, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if err := mustRun(t, t.Context(), pipeline); err != nil {
		t.Fatal(err)
	}
	if got.Load() != n { // sanity: the pipeline actually moved data
		t.Fatalf("want %d delivered, got %d", n, got.Load())
	}

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.bound) != 3 {
		t.Fatalf("want 3 bound instances (producer, transformer, consumer), got %d", len(plugin.bound))
	}
	names := map[sdk.Kind]string{sdk.PRODUCER: "producer", sdk.CONSUMER: "consumer", sdk.TRANSFORMER: "transformer"}
	for i, inst := range plugin.bound {
		if inst.closes.Load() == 0 {
			t.Errorf("bound instance %d (%s) was never closed", i, names[inst.kind])
		}
	}
}
