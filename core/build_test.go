package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// nopBlock is a stand-in sdk.ConfigBlock for resources with no options.
type nopBlock struct{}

func (nopBlock) Origin() sdk.SourceRange { return sdk.SourceRange{SourceName: "test"} }
func (nopBlock) Decode(any) error        { return nil }

// corePlugin returns an in-proc plugin with one resource of each kind:
// a producer emitting `count` copies of `payload`, a counting consumer,
// and a transformer that appends `suffix` to each message.
func corePlugin(name string, payload []byte, count int, consumed *int, suffix string) sdk.Plugin {
	return sdk.NewInProc(name,
		&sdk.Resource{
			Name:  "emit",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
				return func(send chan<- []byte, errs chan<- error) {
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
				return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
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
				return func(in []byte) ([]byte, error) { return append(in, suffix...), nil }, nil
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

func Test_stackTransform(t *testing.T) {
	appendc := func(c byte) sdk.Transformer {
		return func(in []byte) ([]byte, error) { return append(in, c), nil }
	}

	// empty stack is identity
	out, err := stackTransform(nil)([]byte("x"))
	if err != nil || string(out) != "x" {
		t.Fatalf("empty stack: %q, %v", out, err)
	}

	// applied in declaration order
	out, err = stackTransform([]sdk.Transformer{appendc('a'), appendc('b'), appendc('c')})([]byte("_"))
	if err != nil || string(out) != "_abc" {
		t.Fatalf("order: %q, %v", out, err)
	}

	// error propagates and halts the stack
	boom := func([]byte) ([]byte, error) { return nil, fmt.Errorf("boom") }
	if _, err := stackTransform([]sdk.Transformer{appendc('a'), boom, appendc('c')})([]byte("_")); err == nil || err.Error() != "boom" {
		t.Fatalf("want boom, got %v", err)
	}

	// nil (filtered) short-circuits later transformers
	filter := func([]byte) ([]byte, error) { return nil, nil }
	called := false
	spy := func(in []byte) ([]byte, error) { called = true; return in, nil }
	out, err = stackTransform([]sdk.Transformer{filter, spy})([]byte("_"))
	if err != nil || out != nil || called {
		t.Fatalf("filter: out=%q called=%v err=%v", out, called, err)
	}
}

func Test_throttle(t *testing.T) {
	// non-positive: never blocks
	wait := throttle(0)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		wait()
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("throttle(0) blocked")
	}

	// 6000/min = 10ms period; 5 waits must take at least ~40ms
	wait = throttle(6000)
	start = time.Now()
	for i := 0; i < 5; i++ {
		wait()
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("throttle(6000): 5 waits took only %s", elapsed)
	}
}

func Test_applyMetaProducer(t *testing.T) {
	emit := func(n int) sdk.Producer {
		return func(send chan<- []byte, errs chan<- error) {
			for i := 0; i < n; i++ {
				send <- []byte{byte(i)}
			}
			close(send)
		}
	}

	recvAll := func(p sdk.Producer) [][]byte {
		send, errs := make(chan []byte), make(chan error)
		go p(send, errs)
		got := [][]byte{}
		for msg := range send {
			got = append(got, msg)
		}
		return got
	}

	// zero meta: passthrough, all messages arrive
	if got := recvAll(applyMetaProducer(emit(10), sdk.BlockMeta{})); len(got) != 10 {
		t.Fatalf("passthrough: want 10, got %d", len(got))
	}

	// stop-after cuts off exactly at n and closes send
	got := recvAll(applyMetaProducer(emit(10), sdk.BlockMeta{StopAfter: 3}))
	if len(got) != 3 {
		t.Fatalf("stop-after: want 3, got %d", len(got))
	}
	if got[0][0] != 0 || got[2][0] != 2 {
		t.Fatalf("stop-after: wrong messages %v", got)
	}

	// stop-after larger than stream: everything arrives
	if got := recvAll(applyMetaProducer(emit(4), sdk.BlockMeta{StopAfter: 100})); len(got) != 4 {
		t.Fatalf("stop-after overshoot: want 4, got %d", len(got))
	}

	// per-minute paces messages: 6000/min = 10ms period, 5 msgs >= ~40ms
	start := time.Now()
	if got := recvAll(applyMetaProducer(emit(5), sdk.BlockMeta{PerMinute: 6000})); len(got) != 5 {
		t.Fatalf("per-minute: want 5, got %d", len(got))
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("per-minute: not throttled, took %s", elapsed)
	}
}

func Test_applyMetaConsumer(t *testing.T) {
	run := func(meta sdk.BlockMeta, feed int) int {
		count := 0
		consume := func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				count++
			}
			close(done)
		}

		recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
		go applyMetaConsumer(consume, meta)(recv, errs, done)

		stop := make(chan struct{})
		go func() {
			defer close(recv)
			for i := 0; i < feed; i++ {
				select {
				case recv <- []byte{byte(i)}:
				case <-stop:
					return
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("consumer never signalled done")
		}
		close(stop)
		return count
	}

	// zero meta: passthrough
	if got := run(sdk.BlockMeta{}, 10); got != 10 {
		t.Fatalf("passthrough: want 10, got %d", got)
	}

	// stop-after cuts off at n, done still fires
	if got := run(sdk.BlockMeta{StopAfter: 3}, 10); got != 3 {
		t.Fatalf("stop-after: want 3, got %d", got)
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
	err := drain(parse.LiteralResourceFunc(resources...), plugins, func(b parse.Resource, _ sdk.Instance) {
		seen = append(seen, b.Ref)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != n || seen[0] != "transform.suffix.t0" || seen[n-1] != fmt.Sprintf("transform.suffix.t%d", n-1) {
		t.Fatalf("bad drain order/count: %d %v", len(seen), seen)
	}

	// unknown plugin
	err = drain(parse.LiteralResourceFunc(testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{})), plugins, func(parse.Resource, sdk.Instance) {})
	if err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want no-plugin error, got %v", err)
	}

	// bind failure (unknown resource within the plugin)
	err = drain(parse.LiteralResourceFunc(testResource("p", "nonexistent", sdk.PRODUCER, sdk.BlockMeta{})), plugins, func(parse.Resource, sdk.Instance) {})
	if err == nil {
		t.Fatal("want bind error, got nil")
	}

	// stream error propagates
	streamErr := func(int) ([]parse.Resource, error) { return nil, fmt.Errorf("stream broke") }
	if err := drain(streamErr, plugins, func(parse.Resource, sdk.Instance) {}); err == nil || err.Error() != "stream broke" {
		t.Fatalf("want stream error, got %v", err)
	}
}

func Test_BuildPipeline(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("msg"), 5, &consumed, "!")

	// ResourceFunc streams are one-shot; build a fresh description per run
	mksrc := func(producerMeta sdk.BlockMeta) parse.Pipeline {
		return parse.Pipeline{
			Name:         "main",
			Producers:    parse.LiteralResourceFunc(testResource("p", "emit", sdk.PRODUCER, producerMeta)),
			Consumers:    parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
			Transformers: parse.LiteralResourceFunc(testResource("p", "suffix", sdk.TRANSFORMER, sdk.BlockMeta{})),
			ExitOnError:  true,
		}
	}

	src := mksrc(sdk.BlockMeta{})
	src.StopAfter = 7
	pipeline, err := BuildPipeline(src, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.StopAfter != 7 || !pipeline.ExitOnError {
		t.Fatalf("pipeline flags not propagated: %#v", pipeline)
	}
	if err := RunPipeline(pipeline); err != nil {
		t.Fatal(err)
	}
	if consumed != 5 {
		t.Fatalf("want 5 consumed, got %d", consumed)
	}

	// producer meta applies: stop-after 2 of 5
	consumed = 0
	pipeline, err = BuildPipeline(mksrc(sdk.BlockMeta{StopAfter: 2}), []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunPipeline(pipeline); err != nil {
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

	producer := parse.LiteralResourceFunc(testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{}))
	consumer := parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{}))

	_, err := BuildPipeline(parse.Pipeline{Name: "x", Producers: empty, Consumers: consumer, Transformers: empty}, []sdk.Plugin{plugin})
	if err == nil || !strings.Contains(err.Error(), "no producers") {
		t.Fatalf("want no-producers error, got %v", err)
	}

	_, err = BuildPipeline(parse.Pipeline{Name: "x", Producers: producer, Consumers: empty, Transformers: empty}, []sdk.Plugin{plugin})
	if err == nil || !strings.Contains(err.Error(), "no consumers") {
		t.Fatalf("want no-consumers error, got %v", err)
	}

	ghost := parse.LiteralResourceFunc(testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{}))
	_, err = BuildPipeline(parse.Pipeline{Name: "x", Producers: ghost, Consumers: consumer, Transformers: empty}, []sdk.Plugin{plugin})
	if err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want no-plugin error, got %v", err)
	}
}
