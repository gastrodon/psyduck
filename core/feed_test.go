package core

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
)

// Tests for the producer feeder + worker pool (feed.go, stream.go): the run-
// time path that binds producers lazily and runs them replace-on-exhaustion.
// House rules from regression_test.go apply: a hang IS a failure
// (panicSafeRun bounds every run), and a finished run must leave no goroutine
// behind.

// gate coordinates gated producers with the test. Every producer announces
// itself on arrive after bumping inFlight, then blocks until the test sends
// it a release. Driving arrivals and releases one at a time lets a test
// observe exactly how many producers the engine allowed to overlap (maxSeen)
// and prove replace-on-exhaustion: freeing one slot must start the next
// producer even while another is still parked.
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

// await blocks until one producer has arrived at the gate (or ctx ends).
func (g *gate) await(ctx context.Context) {
	select {
	case <-g.arrive:
	case <-ctx.Done():
	}
}

// arrivedWithin reports whether a producer arrives at the gate within d. It is
// a negative check: in a correctly-capped pool no producer may arrive while
// every slot is full, and an over-admitted producer parks at the gate as a
// durable blocked send — so a bounded wait surfaces it deterministically,
// unlike maxSeen's peak-sampling which only catches a transient overlap.
func (g *gate) arrivedWithin(ctx context.Context, d time.Duration) bool {
	select {
	case <-g.arrive:
		return true
	case <-time.After(d):
		return false
	case <-ctx.Done():
		return false
	}
}

// releaseOne frees exactly one parked producer (or gives up when ctx ends).
func (g *gate) releaseOne(ctx context.Context) {
	select {
	case g.release <- struct{}{}:
	case <-ctx.Done():
	}
}

// gatedPlugin's "gated" producer arrives at the gate, waits to be released,
// sends one message, then exits. It is fully ctx-aware: the engine can
// abandon it at any point without leaking it.
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

// The worker pool replaces a finished producer's slot immediately from the
// next arrival — it does not wait for the whole group to drain (as the old
// wave engine did). With three gated producers under a cap of two, freeing
// one slot must start the third while the second is still parked. A wave
// engine would refuse to start the third until both originals exhausted, so
// reaching the third's arrival with the second parked deadlocks it into
// panicSafeRun's timeout.
//
// The cap is proven from both sides: while both slots are full no third
// producer may start (arrivedWithin — an over-admitted producer would park at
// the gate as a durable blocked send, caught deterministically), and once a
// slot frees the third starts at once.
func Test_produce_ReplaceOnExhaustion(t *testing.T) {
	const total, parallel = 3, 2

	g := newGate()
	consumed := 0
	plugins := []sdk.Plugin{gatedPlugin("g", g), corePlugin("p", nil, 0, &consumed, "")}

	producers := make([]parse.Resource, total)
	for i := range producers {
		producers[i] = testResource("g", "gated", sdk.PRODUCER, sdk.BlockMeta{})
	}

	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:            "parallel",
		Producers:       parse.LiteralResourceFunc(producers...),
		Consumers:       parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:    parse.LiteralResourceFunc(),
		ProduceParallel: parallel,
	}, plugins)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		ctx := t.Context()
		g.await(ctx) // both original producers fill the two slots
		g.await(ctx)
		// Both slots are full: no third producer may start. An over-admitted
		// one would already be parked at the gate, so this wait catches it.
		if g.arrivedWithin(ctx, 100*time.Millisecond) {
			t.Errorf("a third producer started while both slots were full: cap %d exceeded", parallel)
		}
		g.releaseOne(ctx) // free one slot; its replacement must start now
		g.await(ctx)      // third producer arrived — replace-on-exhaustion proven
		g.releaseOne(ctx) // let the remaining two finish
		g.releaseOne(ctx)
	}()

	if err := panicSafeRun(t, pipeline); err != nil {
		t.Fatal(err)
	}

	if n := g.starts.Load(); n != total {
		t.Fatalf("want %d producers run, got %d", total, n)
	}
	if n := g.maxSeen.Load(); n != parallel {
		t.Fatalf("want at most %d producers overlapping, got %d", parallel, n)
	}
	if consumed != total {
		t.Fatalf("want %d messages consumed, got %d", total, consumed)
	}
}

// A produce-from pipeline keeps yielding producers for as long as its stream
// does: the bootstrap chunk and every later arrival all run, and all of their
// messages reach the consumer. When the stream exhausts the run ends on its
// own, and the feeder releases the stream before closing feed — so no polling
// is needed to observe the release on the exhaustion path.
func Test_feed_StreamedProducersAllRun(t *testing.T) {
	const chunks, perProducer = 4, 3

	consumed := 0
	plugin := corePlugin("p", []byte("m"), perProducer, &consumed, "")

	var released atomic.Int64
	stream := make([][]parse.Resource, chunks)
	for i := range stream {
		stream[i] = []parse.Resource{testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{})}
	}

	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:            "streamed",
		Producers:       streamOf(&released, stream...),
		Consumers:       parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:    parse.LiteralResourceFunc(),
		ProduceParallel: 1,
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
	if n := released.Load(); n != 1 {
		t.Fatalf("want the stream released exactly once, got %d", n)
	}
}

// A bind error partway through the stream surfaces at run time through the
// pipeline's error reporting: with exit-on-error set, a mid-stream resource
// naming an unknown plugin fails the run, and the stream is still released on
// the way out.
func Test_feed_BindErrorMidStream(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", []byte("m"), 1, &consumed, "")

	var released atomic.Int64
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name: "midstream-error",
		Producers: streamOf(&released,
			[]parse.Resource{testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{})},
			[]parse.Resource{testResource("ghost", "emit", sdk.PRODUCER, sdk.BlockMeta{})},
		),
		Consumers:       parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:    parse.LiteralResourceFunc(),
		ExitOnError:     true,
		ProduceParallel: 1,
	}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}

	err = panicSafeRun(t, pipeline)
	if err == nil || !strings.Contains(err.Error(), `no plugin "ghost"`) {
		t.Fatalf("want the mid-stream bind error to fail the run, got %v", err)
	}

	// The failure cancels the run, so RunPipeline can return while the feeder
	// is still unwinding — poll for the release instead of asserting it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if released.Load() == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stream not released after a failed run: released %d times", released.Load())
}

// A produce-from run cut short mid-stream — a consumer finishing early on
// its own, the same as a caller's cancel — must leave nothing behind: the
// running producer is unwound, the feeder parked in the quiet stream is
// released by its ctx, and the stream itself — and with it the seed — is
// released on the way out. A producer's own stop-after meta only bounds
// that one producer's output, not the pool's decision to keep pulling
// replacements, so the cutoff here has to come from the consumer closing
// done itself (the plugin-owned early-finish path). Follows the
// baseline-poll idiom from regression_test.go.
func Test_feed_NoGoroutineLeak_OnEarlyStop(t *testing.T) {
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
	stopAt5 := sdk.NewInProc("p", &sdk.Resource{
		Name:  "count-early",
		Kinds: sdk.CONSUMER,
		ProvideConsumer: func(sdk.Parser) (sdk.Consumer, error) {
			return func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
				defer close(errs)
				for range recv {
					consumed++
					if consumed >= 5 {
						close(done)
						return
					}
				}
			}, nil
		},
	})
	plugins := []sdk.Plugin{forever, stopAt5}

	baseline := runtime.NumGoroutine()

	var released atomic.Int64
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:            "early-stop",
		Producers:       blockingStream(&released, []parse.Resource{testResource("f", "forever", sdk.PRODUCER, sdk.BlockMeta{})}),
		Consumers:       parse.LiteralResourceFunc(testResource("p", "count-early", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers:    parse.LiteralResourceFunc(),
		ProduceParallel: 1,
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

// A stream that never yields a producer is not an error: it builds fine and
// the run finishes normally (nil), having delivered nothing. The stream is
// still released exactly once on the way out.
func Test_feed_EmptyStream(t *testing.T) {
	consumed := 0
	plugin := corePlugin("p", nil, 0, &consumed, "")

	var released atomic.Int64
	pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
		Name:         "empty",
		Producers:    streamOf(&released),
		Consumers:    parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
		Transformers: parse.LiteralResourceFunc(),
	}, []sdk.Plugin{plugin})
	if err != nil {
		t.Fatalf("empty stream should build, got %v", err)
	}

	if err := panicSafeRun(t, pipeline); err != nil {
		t.Fatalf("empty stream should run to a clean finish, got %v", err)
	}
	if consumed != 0 {
		t.Fatalf("want nothing consumed, got %d", consumed)
	}
	if n := released.Load(); n != 1 {
		t.Fatalf("want the stream released exactly once, got %d", n)
	}
}

// A stream error partway through (the seed dying, a timeout) is an ordinary
// producer error, governed by exit-on-error. With it set the run fails with
// that error; without it the run finishes normally after delivering whatever
// the healthy producers already emitted. Either way the stream is released.
func Test_feed_StreamError(t *testing.T) {
	// errAfterFirst yields one good chunk, then fails on the next pull.
	errAfterFirst := func(released *atomic.Int64) parse.ResourceFunc {
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
				return []parse.Resource{testResource("p", "emit", sdk.PRODUCER, sdk.BlockMeta{})}, nil
			}
			return nil, errors.New("seed exploded")
		}
	}

	for _, tc := range []struct {
		name        string
		exitOnError bool
		wantErr     bool
	}{
		{"exit-on-error fails the run", true, true},
		{"suppressed finishes clean", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumed := 0
			plugin := corePlugin("p", []byte("m"), 1, &consumed, "")

			var released atomic.Int64
			pipeline, err := BuildPipeline(t.Context(), parse.Pipeline{
				Name:            "stream-error",
				Producers:       errAfterFirst(&released),
				Consumers:       parse.LiteralResourceFunc(testResource("p", "count", sdk.CONSUMER, sdk.BlockMeta{})),
				Transformers:    parse.LiteralResourceFunc(),
				ExitOnError:     tc.exitOnError,
				ProduceParallel: 1,
			}, []sdk.Plugin{plugin})
			if err != nil {
				t.Fatal(err)
			}

			err = panicSafeRun(t, pipeline)
			switch {
			case tc.wantErr && (err == nil || !strings.Contains(err.Error(), "seed exploded")):
				t.Fatalf("want the stream error to fail the run, got %v", err)
			case !tc.wantErr && err != nil:
				t.Fatalf("suppressed stream error should finish clean, got %v", err)
			}

			// On the exit-on-error path the failure cancels the run, so the
			// feeder may still be unwinding when RunPipeline returns — poll.
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if released.Load() == 1 {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatalf("stream not released: released %d times", released.Load())
		})
	}
}
