package core

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/flow"
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
	return func(send chan<- []byte, errs chan<- error) {
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
// neither channel — it never finishes on its own.
func emitForever(payload []byte) sdk.Producer {
	return func(send chan<- []byte, errs chan<- error) {
		for {
			send <- payload
		}
	}
}

// countAll consumes everything, counting receipts.
func countAll(got *atomic.Int64) sdk.Consumer {
	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)
		for range recv {
			got.Add(1)
		}
	}
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

			transform := func(msg []byte) ([]byte, error) { return msg, nil }
			if tc.delay {
				transform = func(msg []byte) ([]byte, error) {
					time.Sleep(time.Millisecond)
					return msg, nil
				}
			}

			err := mustRun(t, t.Context(), &Pipeline{
				Producers:   producers,
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
		Producers: []sdk.Producer{func(send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			for i := 0; i < 100; i++ {
				send <- []byte{byte(i)}
			}
		}},
		Consumers: []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) {
			if msg[0]%2 == 0 {
				return nil, nil // filtered
			}
			return msg, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 50 {
		t.Fatalf("want 50 messages past the filter, got %d", n)
	}
}

// Regression for #12: a producer legitimately sending a nil []byte must not
// be mistaken for its channel closing — messages after the nil still flow.
func Test_RunPipeline_nilMessage(t *testing.T) {
	var got atomic.Int64
	err := mustRun(t, t.Context(), &Pipeline{
		Producers: []sdk.Producer{func(send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			send <- []byte("before")
			send <- nil
			send <- []byte("after")
		}},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	// nil transforms to nil and is filtered; the message after it survives
	if n := got.Load(); n != 2 {
		t.Fatalf("stream truncated at nil message: got %d of 2", n)
	}
}

// Regression for #10: a consumer stop-after smaller than the produced count
// used to deadlock the pipeline on a send nobody read.
func Test_RunPipeline_consumerStopAfter(t *testing.T) {
	var got atomic.Int64
	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   []sdk.Producer{emitN(100, []byte("x"), nil)},
		Consumers:   []sdk.Consumer{flow.Consumer(countAll(&got), 0, 0, 3)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 3 {
		t.Fatalf("want exactly 3 consumed, got %d", n)
	}
}

// Pipeline-level stop-after must terminate even an infinite producer.
func Test_RunPipeline_stopAfter(t *testing.T) {
	var got atomic.Int64
	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   []sdk.Producer{emitForever([]byte("x"))},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
		StopAfter:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 5 {
		t.Fatalf("want exactly 5 consumed, got %d", n)
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
		Producers:   []sdk.Producer{emitForever([]byte("x"))},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func Test_RunPipeline_errors(t *testing.T) {
	boom := errors.New("boom")
	erroring := func(side string) sdk.Producer {
		return func(send chan<- []byte, errs chan<- error) {
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
			Producers:   []sdk.Producer{erroring("producer")},
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
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
			Producers:   []sdk.Producer{erroring("producer")},
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
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
			Producers:   []sdk.Producer{emitN(10, []byte("x"), nil)},
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: func(msg []byte) ([]byte, error) { return nil, boom },
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "transformer supplied error") {
			t.Fatalf("want the transformer's error, got %v", err)
		}
	})

	t.Run("consumer error, exit-on-error", func(t *testing.T) {
		consume := func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			defer close(done)
			defer close(errs)
			for range recv {
				errs <- fmt.Errorf("consumer: %w", boom)
			}
		}
		err := mustRun(t, t.Context(), &Pipeline{
			Producers:   []sdk.Producer{emitN(10, []byte("x"), nil)},
			Consumers:   []sdk.Consumer{consume},
			Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "consumer supplied error") {
			t.Fatalf("want the consumer's error, got %v", err)
		}
	})
}

// Regression for #19's panic hazard: an error emitted after the data channel
// closes must never crash the engine, whether or not it can still be
// reported.
func Test_RunPipeline_lateError(t *testing.T) {
	late := func(send chan<- []byte, errs chan<- error) {
		send <- []byte("x")
		close(send)
		errs <- errors.New("late cleanup failure")
		close(errs)
	}
	var got atomic.Int64
	if err := mustRun(t, t.Context(), &Pipeline{
		Producers:   []sdk.Producer{late, emitN(5, []byte("y"), nil)},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 6 {
		t.Fatalf("want 6 messages, got %d", n)
	}
}

// Regression for #11/#19: a run the engine completes must release every
// goroutine it started — the old join machinery leaked a fixed set per run,
// even fully successful ones. Runs that abandon a producer mid-send park
// that producer's own goroutine (the sdk contract has no cancellation), so
// this measures well-behaved runs, where zero engine goroutines may remain.
func Test_RunPipeline_goroutineBounded(t *testing.T) {
	pipeline := func() *Pipeline {
		var got atomic.Int64
		return &Pipeline{
			Producers: []sdk.Producer{
				emitN(50, []byte("x"), nil),
				func(send chan<- []byte, errs chan<- error) {
					defer close(send)
					defer close(errs)
					send <- []byte("y")
					errs <- errors.New("mid-stream failure")
				},
			},
			Consumers:   []sdk.Consumer{countAll(&got), countAll(&got)},
			Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
		}
	}

	baseline := runtime.NumGoroutine()
	for i := 0; i < 25; i++ {
		mustRun(t, t.Context(), pipeline())
	}

	// give parked goroutines a moment to unwind, then compare
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	t.Fatalf("goroutines leaked across runs: %d -> %d\n%s",
		baseline, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
}
