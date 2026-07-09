package core

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// Tests for the meta producer (meta.go): the single sdk.Producer that
// BuildPipeline emits for produce-from and produce-from-parallel pipelines.
// House rules from regression_test.go apply: a hang IS a failure
// (panicSafeRun bounds every run), and a finished run must leave no
// goroutine behind.

// gate coordinates gated producers with the test. Every producer announces
// itself on arrive after bumping inFlight, then blocks until the test sends
// it a release. Assembling a full wave at that barrier before releasing
// anyone makes the concurrency observation deterministic: maxSeen records
// exactly how many producers the engine allowed to overlap, and an
// over-serialized engine (a wave smaller than expected) deadlocks into
// panicSafeRun's timeout instead of passing by luck.
type gate struct {
	arrive   chan struct{}
	release  chan struct{}
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	starts   atomic.Int64
}

func newGate() *gate {
	return &gate{arrive: make(chan struct{}), release: make(chan struct{})}
}

// run assembles and releases waves of the given sizes, in order, giving up
// when ctx ends (the test failing or timing out).
func (g *gate) run(ctx context.Context, sizes ...int) {
	for _, size := range sizes {
		for range size {
			select {
			case <-g.arrive:
			case <-ctx.Done():
				return
			}
		}
		for range size {
			select {
			case g.release <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// gatedPlugin's "gated" producer sends one message once released by g. It is
// fully ctx-aware: the engine can abandon it at any point without leaking it.
func gatedPlugin(name string, g *gate) sdk.Plugin {
	return sdk.NewInProc(name, &sdk.Resource{
		Name:  "gated",
		Kinds: sdk.PRODUCER,
		ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
			return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
				defer close(send)
				defer close(errs)
				g.starts.Add(1)
				n := g.inFlight.Add(1)
				defer g.inFlight.Add(-1)
				for {
					m := g.maxSeen.Load()
					if n <= m || g.maxSeen.CompareAndSwap(m, n) {
						break
					}
				}
				select {
				case g.arrive <- struct{}{}:
				case <-ctx.Done():
					return
				}
				select {
				case <-g.release:
				case <-ctx.Done():
					return
				}
				select {
				case send <- []byte("g"):
				case <-ctx.Done():
				}
			}, nil
		},
	})
}

// streamOf is a produce-from-shaped ResourceFunc: each call yields the next
// chunk, then exhaustion. A release call (max < 1) kills the stream and
// counts into released.
func streamOf(released *atomic.Int64, chunks ...[]parse.Resource) parse.ResourceFunc {
	pos := 0
	return func(ctx context.Context, max int) ([]parse.Resource, error) {
		if max < 1 {
			released.Add(1)
			pos = len(chunks)
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pos >= len(chunks) {
			return nil, nil
		}
		chunk := chunks[pos]
		pos++
		return chunk, nil
	}
}

// blockingStream yields its one chunk, then parks until the pull's ctx ends
// — a produce-from seed gone quiet. A release call (max < 1) kills it and
// counts into released; afterwards it only reports exhaustion.
func blockingStream(released *atomic.Int64, first []parse.Resource) parse.ResourceFunc {
	var delivered, dead bool
	return func(ctx context.Context, max int) ([]parse.Resource, error) {
		if max < 1 {
			dead = true
			released.Add(1)
			return nil, nil
		}
		if dead {
			return nil, nil
		}
		if !delivered {
			delivered = true
			return first, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

// A produce-from-parallel pipeline must run its producers in sequential
// groups of at most the configured size. Six gated producers under a cap of
// 2 must overlap exactly two at a time, wave after wave, and every message
// must still reach the consumer.
func Test_BuildPipeline_ProduceFromParallel(t *testing.T) {
	const total, parallel = 6, 2

	g := newGate()
	consumed := 0
	plugins := []sdk.Plugin{gatedPlugin("g", g), corePlugin("p", nil, 0, &consumed, "")}

	producers := make([]parse.Resource, total)
	for i := range producers {
		producers[i] = testResource("g", "gated", sdk.PRODUCER, sdk.BlockMeta{})
	}

	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:                "parallel",
		Producers:           parse.LiteralResourceFunc(producers...),
		Consumers:           parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:        parse.LiteralResourceFunc(),
		ProduceFromParallel: parallel,
	}, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(pipeline.Producers); n != 1 {
		t.Fatalf("want 1 meta producer, got %d", n)
	}

	go g.run(t.Context(), parallel, parallel, parallel)
	if err := panicSafeRun(t, pipeline); err != nil {
		t.Fatal(err)
	}

	if n := g.starts.Load(); n != total {
		t.Fatalf("want %d producers run, got %d", total, n)
	}
	if n := g.maxSeen.Load(); n != parallel {
		t.Fatalf("want exactly %d producers overlapping, got %d", parallel, n)
	}
	if consumed != total {
		t.Fatalf("want %d messages consumed, got %d", total, consumed)
	}
}

// A produce-from pipeline keeps yielding producers for as long as its stream
// does: the bootstrap chunk and every later arrival all run, and all of
// their messages reach the consumer. When the stream exhausts, the run ends
// on its own and the stream is released before the meta producer closes its
// stream — the meta producer owns the seed's lifetime.
func Test_MetaProducer_StreamedProducersAllRun(t *testing.T) {
	const chunks, perProducer = 4, 3

	consumed := 0
	plugin := corePlugin("p", []byte("m"), perProducer, &consumed, "")

	var released atomic.Int64
	stream := make([][]parse.Resource, chunks)
	for i := range stream {
		stream[i] = []parse.Resource{testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{})}
	}

	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:         "streamed",
		Producers:    streamOf(&released, stream...),
		Consumers:    parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers: parse.LiteralResourceFunc(),
		Spec:         parse.PipelineSpec{RemoteSeed: &parse.Resource{Ref: "produce.seed.t"}},
	}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	if err := panicSafeRun(t, pipeline); err != nil {
		t.Fatal(err)
	}

	if want := chunks * perProducer; consumed != want {
		t.Fatalf("want %d messages consumed, got %d", want, consumed)
	}
	// The run ended by exhaustion, so the meta producer released the stream
	// before closing send — no polling needed on this path.
	if n := released.Load(); n != 1 {
		t.Fatalf("want the stream released exactly once, got %d", n)
	}
}

// Bind errors past BuildPipeline's bootstrap peek surface at run time
// through the pipeline's error reporting: with exit-on-error set, a
// mid-stream resource naming an unknown plugin fails the run, and the
// stream is still released on the way out.
func Test_MetaProducer_BindErrorMidStream(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("m"), 1, &consumed, "")

	var released atomic.Int64
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name: "midstream-error",
		Producers: streamOf(&released,
			[]parse.Resource{testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{})},
			[]parse.Resource{testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{})},
		),
		Consumers:    parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers: parse.LiteralResourceFunc(),
		Spec:         parse.PipelineSpec{RemoteSeed: &parse.Resource{Ref: "produce.seed.t"}},
		ExitOnError:  true,
	}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}

	err = panicSafeRun(t, pipeline)
	if err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want the mid-stream bind error to fail the run, got %v", err)
	}

	// The failure cancels the run, so RunPipeline can return while the meta
	// producer is still unwinding — poll for the release instead of
	// asserting it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if released.Load() == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stream not released after a failed run: released %d times", released.Load())
}

// A produce-from run cut short mid-stream (StopAfter here; a caller's cancel
// is the same path) must leave nothing behind: the running group is unwound,
// the puller parked in the quiet stream is released by its ctx, and the
// stream itself — and with it the seed — is released on the way out. Follows
// the baseline-poll idiom from regression_test.go.
func Test_MetaProducer_NoGoroutineLeak_OnEarlyStop(t *testing.T) {
	forever := sdk.NewInProc("f", &sdk.Resource{
		Name:  "forever",
		Kinds: sdk.PRODUCER,
		ProvideProducer: func(sdk.Parser) (sdk.Producer, error) {
			return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
				defer close(send)
				defer close(errs)
				for {
					select {
					case send <- []byte("x"):
					case <-ctx.Done():
						return
					}
				}
			}, nil
		},
	})
	consumed := 0
	plugins := []sdk.Plugin{forever, corePlugin("p", nil, 0, &consumed, "")}

	baseline := runtime.NumGoroutine()

	var released atomic.Int64
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:         "early-stop",
		Producers:    blockingStream(&released, []parse.Resource{testResource("f", "forever", sdk.PRODUCER, sdk.BlockMeta{})}),
		Consumers:    parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers: parse.LiteralResourceFunc(),
		Spec:         parse.PipelineSpec{RemoteSeed: &parse.Resource{Ref: "produce.seed.t"}},
		StopAfter:    5,
	}, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if err := panicSafeRun(t, pipeline); err != nil {
		t.Fatal(err)
	}
	if consumed != 5 {
		t.Fatalf("want 5 consumed before the stop, got %d", consumed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if released.Load() == 1 && runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	t.Fatalf("early-stopped produce-from run left residue: released=%d goroutines %d -> %d\n%s",
		released.Load(), baseline, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
}
