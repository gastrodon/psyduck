package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

// runTimeout guards every RunPipeline call in this file: the old engine's
// failure modes were deadlocks, so a hang IS the regression.
const runTimeout = 10 * time.Second

func mustRun(t *testing.T, ctx context.Context, p *Pipeline) error {
	t.Helper()
	got := make(chan error, 1)
	go func() { got <- RunPipeline(ctx, p) }()
	select {
	case err := <-got:
		return err
	case <-time.After(runTimeout):
		t.Fatal("RunPipeline did not finish: pipeline deadlocked")
		return nil
	}
}

// emitN produces count copies of payload, counting sends, closing both
// channels on the way out like the stdlib producers do.
func emitN(count int, payload []byte, sent *atomic.Int64) sdk.Producer {
	return func(_ context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		for i := 0; i < count; i++ {
			send <- payload
			if sent != nil {
				sent.Add(1)
			}
		}
	}
}

// emitForever produces payload until nothing receives anymore. It closes
// neither channel and deliberately ignores ctx — it never finishes on its
// own, unlike a well-behaved plugin. Tests use it to prove RunPipeline
// itself bounds cancellation rather than relying on plugin cooperation with
// the sdk's context contract.
func emitForever(payload []byte) sdk.Producer {
	return func(_ context.Context, send chan<- []byte, errs chan<- error) {
		for {
			send <- payload
		}
	}
}

// countAll consumes everything, counting receipts.
func countAll(got *atomic.Int64) sdk.Consumer {
	return func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)
		for range recv {
			got.Add(1)
		}
	}
}

// staticSource adapts a fixed set of producers into a ProducerSource, the way
// a literal (produce = [...]) pipeline's feeder does: send each producer into
// the feed channel then close it, with an empty error stream.
func staticSource(ps ...sdk.Producer) ProducerSource {
	return func(ctx context.Context) (<-chan sdk.Producer, <-chan error) {
		feed := make(chan sdk.Producer)
		errs := make(chan error)
		go func() {
			defer close(errs)
			defer close(feed)
			for _, p := range ps {
				select {
				case feed <- p:
				case <-ctx.Done():
					return
				}
			}
		}()
		return feed, errs
	}
}

// Test_RunPipeline_noTransformer covers RunPipeline's Transformer==nil fast
// path — the bypass loop that forwards producers straight to the sink with no
// transform stage. It is the common zero-transformer pipeline
// (composeTransformers returns nil for an empty stack), yet every other
// RunPipeline test injects a non-nil transformer, so nothing else runs it.
func Test_RunPipeline_noTransformer(t *testing.T) {
	t.Run("passes every message straight through", func(t *testing.T) {
		const n = 1000
		var got atomic.Int64
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(emitN(n, []byte("x"), nil)),
			Parallel:    1,
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Load() != n {
			t.Fatalf("want %d delivered, got %d", n, got.Load())
		}
	})

	t.Run("producer error is attributed, exit-on-error", func(t *testing.T) {
		boom := errors.New("boom")
		erroring := func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			send <- []byte("one")
			errs <- boom
		}
		var got atomic.Int64
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(erroring),
			Parallel:    1,
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: nil,
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "producer supplied error") {
			t.Fatalf("want attributed producer error, got %v", err)
		}
	})
}

func Test_RunPipeline(t *testing.T) {
	cases := []struct {
		name                 string
		count                int
		producers, consumers int
		delay                bool
	}{
		{"1x1", 10_000, 1, 1, false},
		{"1x10", 10_000, 1, 10, false},
		{"10x1", 10_000, 10, 1, false},
		{"10x10", 1_000, 10, 10, false},
		{"slow transformer", 100, 10, 10, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := make([]atomic.Int64, tc.producers)
			producers := make([]sdk.Producer, tc.producers)
			for i := range producers {
				producers[i] = emitN(tc.count, []byte{byte(i)}, &sent[i])
			}

			got := make([]atomic.Int64, tc.consumers)
			consumers := make([]sdk.Consumer, tc.consumers)
			for i := range consumers {
				consumers[i] = countAll(&got[i])
			}

			transform := sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil })
			if tc.delay {
				transform = sdk.Map(func(msg []byte) ([]byte, error) {
					time.Sleep(time.Millisecond)
					return msg, nil
				})
			}

			err := mustRun(t, t.Context(), &Pipeline{
				Producers:   staticSource(producers...),
				Parallel:    tc.producers,
				Consumers:   consumers,
				Transformer: transform,
			})
			if err != nil {
				t.Fatal(err)
			}

			for i := range sent {
				if n := sent[i].Load(); n != int64(tc.count) {
					t.Errorf("producer %d: sent %d of %d", i, n, tc.count)
				}
			}
			want := int64(tc.count * tc.producers)
			for i := range got {
				if n := got[i].Load(); n != want {
					t.Errorf("consumer %d: got %d of %d", i, n, want)
				}
			}
		})
	}
}

func Test_RunPipeline_filtering(t *testing.T) {
	var got atomic.Int64
	err := mustRun(t, t.Context(), &Pipeline{
		Producers: staticSource(func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			for i := 0; i < 100; i++ {
				send <- []byte{byte(i)}
			}
		}),
		Parallel:  1,
		Consumers: []sdk.Consumer{countAll(&got)},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) {
			if msg[0]%2 == 0 {
				return nil, nil // filtered
			}
			return msg, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 50 {
		t.Fatalf("want 50 messages past the filter, got %d", n)
	}
}

// Cancelling the context stops a pipeline whose producer would never finish.
func Test_RunPipeline_cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var got atomic.Int64
	err := mustRun(t, ctx, &Pipeline{
		Producers:   staticSource(emitForever([]byte("x"))),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func Test_RunPipeline_errors(t *testing.T) {
	boom := errors.New("boom")
	erroring := func(side string) sdk.Producer {
		return func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			send <- []byte("one")
			errs <- fmt.Errorf("%s: %w", side, boom)
			send <- []byte("two")
		}
	}

	t.Run("producer error, exit-on-error", func(t *testing.T) {
		var got atomic.Int64
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(erroring("producer")),
			Parallel:    1,
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
			ExitOnError: true,
		})
		if !errors.Is(err, boom) {
			t.Fatalf("want the producer's error, got %v", err)
		}
		if !strings.Contains(err.Error(), "producer supplied error") {
			t.Fatalf("unattributed error: %v", err)
		}
	})

	t.Run("producer error, keep going", func(t *testing.T) {
		var got atomic.Int64
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(erroring("producer")),
			Parallel:    1,
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		})
		if err != nil {
			t.Fatalf("errors are logged, not returned, without exit-on-error: %v", err)
		}
		if n := got.Load(); n != 2 {
			t.Fatalf("stream should survive the error: got %d of 2", n)
		}
	})

	t.Run("transformer error, exit-on-error", func(t *testing.T) {
		var got atomic.Int64
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(emitN(10, []byte("x"), nil)),
			Parallel:    1,
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return nil, boom }),
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "transformer supplied error") {
			t.Fatalf("want the transformer's error, got %v", err)
		}
	})

	t.Run("consumer error, exit-on-error", func(t *testing.T) {
		consume := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			defer close(done)
			defer close(errs)
			for range recv {
				errs <- fmt.Errorf("consumer: %w", boom)
			}
		}
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   staticSource(emitN(10, []byte("x"), nil)),
			Parallel:    1,
			Consumers:   []sdk.Consumer{consume},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "consumer supplied error") {
			t.Fatalf("want the consumer's error, got %v", err)
		}
	})
}

// The tests below drive a multi-stage composeTransformers chain through the
// full RunPipeline/startTransform machinery. They are coverage for the
// concurrent chaining introduced by the transformer channel-contract rewrite
// (issue #22), not regressions: the old stackTransform composed synchronous
// per-message funcs by recursion, so it had no chain teardown/deadlock failure
// mode to reproduce. build_test.go only exercises composeTransformers directly
// with a buffered errs channel and context.Background(); these fill the gap by
// running real chains with the unbuffered errs and real cancellation.

// appendByte is a well-behaved stdlib-style transformer stage.
func appendByte(c byte) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- append(append([]byte{}, msg...), c):
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// A real 3-stage chain through the full engine delivers every message,
// transformed in declaration order, and terminates.
func Test_RunPipeline_ChainFullRun(t *testing.T) {
	const n = 5000
	var got atomic.Int64
	chain := composeTransformers([]sdk.Transformer{appendByte('a'), appendByte('b'), appendByte('c')})

	var seen atomic.Value
	consume := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)
		for msg := range recv {
			got.Add(1)
			seen.Store(string(msg))
		}
	}

	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   staticSource(emitN(n, []byte("_"), nil)),
		Parallel:    1,
		Consumers:   []sdk.Consumer{consume},
		Transformer: chain,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Load() != n {
		t.Fatalf("want %d messages, got %d", n, got.Load())
	}
	if s := seen.Load().(string); s != "_abc" {
		t.Fatalf("want _abc, got %q", s)
	}
}

// Cancelling mid-stream must tear down a multi-stage chain fed by a
// ctx-ignoring, never-closing producer. A hang here means the chain leaks.
func Test_RunPipeline_ChainCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	var got atomic.Int64
	chain := composeTransformers([]sdk.Transformer{appendByte('a'), appendByte('b'), appendByte('c')})
	err := mustRun(t, ctx, &Pipeline{
		Producers:   staticSource(emitForever([]byte("x"))),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: chain,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A consumer that finishes after one message triggers the early-break path in
// RunPipeline. With a multi-stage chain still mid-flight and an infinite
// producer, the engine must cancel and join without hanging.
func Test_RunPipeline_ChainEarlyConsumerFinish(t *testing.T) {
	oneShot := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(errs)
		<-recv      // take exactly one
		close(done) // signal finished; sink should stop feeding us
	}
	chain := composeTransformers([]sdk.Transformer{appendByte('a'), appendByte('b'), appendByte('c')})
	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   staticSource(emitForever([]byte("x"))),
		Parallel:    1,
		Consumers:   []sdk.Consumer{oneShot},
		Transformer: chain,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("early consumer finish should end cleanly, got %v", err)
	}
}

// A middle stage blocked in a slow op when ctx is cancelled must still exit;
// exercises ctx.Done racing an in-flight send between stages.
func Test_RunPipeline_ChainSlowMiddleCancel(t *testing.T) {
	slow := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				select {
				case <-time.After(5 * time.Millisecond):
				case <-ctx.Done():
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
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	var got atomic.Int64
	chain := composeTransformers([]sdk.Transformer{appendByte('a'), slow, appendByte('c')})
	err := mustRun(t, ctx, &Pipeline{
		Producers:   staticSource(emitForever([]byte("x"))),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: chain,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// A middle stage that emits an error on every message, driven through the real
// (unbuffered, single-forwarder) errs path with ExitOnError. The first error
// must cancel and tear the whole chain down and surface as the run error.
func Test_RunPipeline_ChainMiddleErrorExit(t *testing.T) {
	boom := errors.New("boom")
	errEvery := func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case _, ok := <-in:
				if !ok {
					return
				}
				select {
				case errs <- boom:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
	var got atomic.Int64
	chain := composeTransformers([]sdk.Transformer{appendByte('a'), errEvery, appendByte('c')})
	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   staticSource(emitForever([]byte("x"))),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: chain,
		ExitOnError: true,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom from the middle stage, got %v", err)
	}
}
