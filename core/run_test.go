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
// itself bounds StopAfter/cancellation rather than relying on plugin
// cooperation with the sdk's context contract.
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

// testMapTransform adapts a per-message function into the channel-based
// sdk.Transformer contract: a (nil, nil) result filters the message out, an
// error is reported on errs (skipping that message) without halting the
// stream. It exists only to keep these engine tests terse — stdlib itself
// has no such adapter; every real transformer owns its raw loop.
func testMapTransform(f func(msg []byte) ([]byte, error)) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				transformed, err := f(msg)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if transformed == nil {
					continue
				}
				select {
				case out <- transformed:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
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

			transform := testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil })
			if tc.delay {
				transform = testMapTransform(func(msg []byte) ([]byte, error) {
					time.Sleep(time.Millisecond)
					return msg, nil
				})
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
		Producers: []sdk.Producer{func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			for i := 0; i < 100; i++ {
				send <- []byte{byte(i)}
			}
		}},
		Consumers: []sdk.Consumer{countAll(&got)},
		Transformer: testMapTransform(func(msg []byte) ([]byte, error) {
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

// Pipeline-level stop-after must terminate even an infinite producer.
func Test_RunPipeline_stopAfter(t *testing.T) {
	var got atomic.Int64
	err := mustRun(t, t.Context(), &Pipeline{
		Producers:   []sdk.Producer{emitForever([]byte("x"))},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil }),
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
		Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil }),
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
			Producers:   []sdk.Producer{erroring("producer")},
			Consumers:   []sdk.Consumer{countAll(&got)},
			Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil }),
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
			Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil }),
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
			Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return nil, boom }),
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
			Producers:   []sdk.Producer{emitN(10, []byte("x"), nil)},
			Consumers:   []sdk.Consumer{consume},
			Transformer: testMapTransform(func(msg []byte) ([]byte, error) { return msg, nil }),
			ExitOnError: true,
		})
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "consumer supplied error") {
			t.Fatalf("want the consumer's error, got %v", err)
		}
	})
}
