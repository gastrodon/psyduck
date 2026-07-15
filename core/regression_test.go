package core

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/flow"
	"github.com/psyduck-etl/sdk"
)

// This file holds regression tests for the concurrency bugs the pre-rewrite
// engine had (join()/joinProducers/joinConsumers in core/build.go, the
// four-goroutine RunPipeline in core/run.go — both deleted by the rewrite
// that introduced this file). Each test below reproduces the exact scenario
// that used to panic or deadlock that engine.
//
// That isn't a claim taken on faith: before writing these, the deleted
// implementation (git show 5c9985f:core/{build,run}.go) was checked out
// into an isolated module and run directly against these same scenarios.
// Results:
//
//   - the #19/#11 scenario in Test_LateErrorAfterExitOnError_NoPanic
//     deterministically panicked the old engine with "send on closed
//     channel", inside the goroutine that forwards errorProducer into the
//     aggregate errs channel (old run.go's RunPipeline.func1) — the
//     forwarder blocks because RunPipeline already returned and stopped
//     reading errs, then panics the instant the data-forwarding goroutine
//     closes that same channel out from under it.
//   - the #10 scenario in Test_ConsumerEarlyFinish_NoDeadlock hung the old
//     engine indefinitely (confirmed blocked past a 3s bound): a consumer
//     that stopped reading its recv channel early (closing done itself,
//     without draining the rest) left RunPipeline's forward loop blocked
//     forever on a send nobody would ever read again.
//   - the #12 scenario in Test_NilMessage_DoesNotTruncateStream silently
//     dropped every message after the nil — including ones from producers
//     that hadn't even sent it — because old joinProducers used msg == nil
//     on the joined channel as its "all producers closed" sentinel.
//
// #8 (produce-from seed closing without sending) is a parse-layer bug, not
// a core-engine one; its regression test lives in
// parse/hcl/hcl_test.go:TestParseProduceFromClosedSeed.
//
// One test here — Test_ErrorAfterDataClose_IsDelivered — guards a dropped-error
// bug in the *new* engine's error forwarder that was found and fixed during the
// rewrite, not in the 5c9985f engine reproduced above. It has no issue number
// and isn't one of #10/#11/#12/#19, but it's a genuine regression against a
// real fix, so it lives with the rest.
//
// Capability/invariant tests that exercise behavior the rewrite *introduced*
// (the ctx-aware exit path, the transformer channel stage) rather than
// reproducing a deleted-engine bug are not regressions and live with the other
// RunPipeline behavior tests in run_test.go, not here.
//
// sdk v0.5.1 added ctx as Producer/Consumer's first parameter, giving
// well-behaved plugins a way to exit on cancellation instead of parking.
// The producers below accept and deliberately ignore it — these tests exist
// to prove the engine itself never panics or deadlocks even when a plugin
// doesn't cooperate, not to exercise cancellation.

// panicSafeRun runs RunPipeline in its own goroutine, converting any panic
// into a clean test failure instead of crashing the whole test binary, and
// fails if RunPipeline doesn't return within runTimeout (a hang is exactly
// what these tests exist to catch).
func panicSafeRun(t *testing.T, p *Pipeline) error {
	t.Helper()
	type outcome struct {
		err   error
		panic any
		stack []byte
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 1<<16)
				done <- outcome{panic: r, stack: buf[:runtime.Stack(buf, false)]}
			}
		}()
		done <- outcome{err: RunPipeline(t.Context(), p)}
	}()

	select {
	case o := <-done:
		if o.panic != nil {
			t.Fatalf("RunPipeline panicked: %v\n%s", o.panic, o.stack)
		}
		return o.err
	case <-time.After(runTimeout):
		t.Fatal("RunPipeline did not finish: pipeline deadlocked")
		return nil
	}
}

// Regression for #11 and #19: a producer that errors twice — once early
// (triggering ExitOnError) and once late, well after RunPipeline has
// already returned — must not crash the process. The old engine's
// error-forwarding goroutine kept running after RunPipeline returned
// (nothing ever stopped it), so the late error either parked that
// goroutine forever (leak) or, if it arrived while the aggregate errs
// channel was mid-close, panicked with "send on closed channel" — see the
// file-level comment for the confirmed reproduction.
func Test_LateErrorAfterExitOnError_NoPanic(t *testing.T) {
	const lateDelay = 100 * time.Millisecond

	// ctx is ignored on purpose: err2 must still be attempted after
	// RunPipeline has cancelled and returned, which is exactly what a
	// ctx-respecting producer would avoid.
	producer := func(_ context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		send <- []byte("x")
		errs <- errors.New("err1")
		time.Sleep(lateDelay)
		errs <- errors.New("err2 (arrives after RunPipeline already returned)")
	}
	consumer := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)
		for range recv {
		}
	}

	start := time.Now()
	err := panicSafeRun(t, &Pipeline{
		Producers:   staticSource(producer),
		Parallel:    1,
		Consumers:   []sdk.Consumer{consumer},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		ExitOnError: true,
	})
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "err1") {
		t.Fatalf("want err1 to abort the run, got %v", err)
	}
	if elapsed >= lateDelay {
		t.Fatalf("RunPipeline waited for the late error instead of returning on the first one: took %s", elapsed)
	}

	// err2 is sent lateDelay after err1; give it time to arrive at whatever
	// (now-cancelled) forwarder would have received it, so a reintroduced
	// panic happens inside this test's window instead of silently later.
	time.Sleep(2 * lateDelay)
	t.Log("survived the late error without panicking")
}

// Regression for #10: a consumer that finishes early on its own — closing
// done well before the producer runs out — used to deadlock the pipeline
// permanently if it broke out of its receive loop without draining the
// rest, so whatever kept sending into it blocked forever on a send nobody
// would ever read again. The sink must stop sending to a finished consumer
// instead of blocking the pipeline on it.
func Test_ConsumerEarlyFinish_NoDeadlock(t *testing.T) {
	var got atomic.Int64
	consumer := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(errs)
		for range recv {
			if got.Add(1) >= 3 {
				close(done)
				return
			}
		}
	}
	err := panicSafeRun(t, &Pipeline{
		Producers:   staticSource(emitN(100, []byte("x"), nil)),
		Parallel:    1,
		Consumers:   []sdk.Consumer{consumer},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := got.Load(); n != 3 {
		t.Fatalf("want exactly 3 consumed, got %d", n)
	}
}

// Regression for #12: with two or more producers, the old joinProducers
// used msg == nil on its merged channel as the "all producers closed"
// sentinel — indistinguishable from a producer legitimately sending a nil
// []byte. That truncated the whole stream early, silently dropping every
// later message from every producer, not just the one that sent the nil.
// (A single producer bypassed the join entirely in the old code, so this
// needs at least two to actually exercise the bug.)
func Test_NilMessage_DoesNotTruncateStream(t *testing.T) {
	var got atomic.Int64
	second := make(chan struct{})
	producers := []sdk.Producer{
		func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			send <- []byte("before")
			send <- nil
			send <- []byte("after")
		},
		func(_ context.Context, send chan<- []byte, errs chan<- error) {
			defer close(send)
			defer close(errs)
			<-second // stays open past the first producer's nil message
			send <- []byte("second-producer-message")
		},
	}
	consumer := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)
		for range recv {
			if got.Add(1) == 2 {
				close(second) // let the second producer send once "after" arrived
			}
		}
	}

	err := panicSafeRun(t, &Pipeline{
		Producers:   staticSource(producers...),
		Parallel:    2,
		Consumers:   []sdk.Consumer{consumer},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	// producer 1 sends "before", a filtered-out nil, then "after"; "after"
	// arriving is what unblocks producer 2 to send its own message. Seeing
	// all 3 ("before", "after", "second-producer-message") proves the nil
	// didn't truncate the stream for either producer.
	if n := got.Load(); n != 3 {
		t.Fatalf("stream truncated at the nil sentinel: got %d messages, want 3", n)
	}
}

// Regression for #11/#19: a run the engine completes must release every
// goroutine it started — the old join machinery leaked a fixed set per run,
// even fully successful ones, and every failed ExitOnError run leaked its
// error-forwarding goroutines permanently (see
// Test_LateErrorAfterExitOnError_NoPanic above for why that could also
// panic). The erroring producer below finishes on its own every time (it
// never blocks on ctx), so this specifically isolates the engine's own
// bookkeeping from plugin cancellation behavior.
func Test_GoroutinesDoNotAccumulateAcrossRuns(t *testing.T) {
	pipeline := func() *Pipeline {
		var got atomic.Int64
		return &Pipeline{
			Producers: staticSource(
				emitN(50, []byte("x"), nil),
				func(_ context.Context, send chan<- []byte, errs chan<- error) {
					defer close(send)
					defer close(errs)
					send <- []byte("y")
					errs <- errors.New("mid-stream failure")
				},
			),
			Parallel:    2,
			Consumers:   []sdk.Consumer{countAll(&got), countAll(&got)},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		}
	}

	baseline := runtime.NumGoroutine()
	for i := 0; i < 25; i++ {
		panicSafeRun(t, pipeline())
	}

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

// Regression for the late-error drop: a producer that closes its data
// channel and then reports an error. The error forwarder used to sweep
// pending errors with a non-blocking default the moment all data channels
// closed — but on an unbuffered errs channel a plugin blocked mid-send has
// nothing "pending in the channel", so the sweep saw nothing and the error
// was silently lost (and the plugin stayed parked on the send until
// pipeline cancellation). The error must instead be delivered: the
// forwarder now waits for the producer function to return before its final
// race-free drain.
func Test_ErrorAfterDataClose_IsDelivered(t *testing.T) {
	producer := func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(errs)
		send <- []byte("x")
		close(send)
		// Give the old sweep every chance to run first, so the drop is
		// deterministic rather than a lucky race.
		time.Sleep(50 * time.Millisecond)
		select {
		case errs <- errors.New("late error after data close"):
		case <-ctx.Done():
		}
	}

	var got atomic.Int64
	err := panicSafeRun(t, &Pipeline{
		Producers:   staticSource(producer),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		ExitOnError: true,
	})
	if err == nil || !strings.Contains(err.Error(), "late error after data close") {
		t.Fatalf("late error was dropped: got %v", err)
	}
}

// stream.go's error forwarder stops the moment the producer *function*
// returns, relying on the documented invariant (stream.go:114-116) that an
// unbuffered errs send is received before the function can return.
// flow.Producer breaks that invariant: at the stop-after cutoff the wrapper
// returns immediately, cancelling the derived ctx to tell the inner plugin —
// which shares the same errs channel — to wind down. A plugin that reports
// why it stopped (failed close handshake, flush error) sends into an errs
// channel nobody reads anymore.
//
// The inner producer below is contrived to hit the window deterministically
// rather than by luck: it waits for the cutoff cancel, then gives the
// engine's forwarder every chance to observe the wrapper's return before
// reporting (same trick as Test_ErrorAfterDataClose_IsDelivered above).
func Test_StopAfterTeardownError_IsDelivered(t *testing.T) {
	t.Skip("gastrodon/psyduck#37: joining the inner producer before flow.Producer " +
		"returns deadlocks against producers that don't select on ctx.Done() " +
		"(e.g. a blind `send <- msg` loop) — needs a design that doesn't assume " +
		"ctx-cooperative producers; see issue for the goroutine-dump writeup")
	inner := func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		for i := 0; i < 3; i++ {
			select {
			case send <- []byte("x"):
			case <-ctx.Done():
				return
			}
		}
		// The wrapper cancels this ctx as it returns at the cutoff.
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)
		select {
		case errs <- errors.New("teardown failure at stop-after cutoff"):
		case <-time.After(time.Second):
			// nobody is listening anymore — this timeout firing IS the bug;
			// the assertion below reports it as the missing pipeline error.
		}
	}

	var got atomic.Int64
	err := panicSafeRun(t, &Pipeline{
		Producers:   staticSource(flow.Producer(inner, 0, 0, 3)),
		Parallel:    1,
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		ExitOnError: true,
	})
	if err == nil || !strings.Contains(err.Error(), "teardown failure at stop-after cutoff") {
		t.Fatalf("stop-after teardown error was dropped: got %v", err)
	}
}

// BuildPipeline wraps every per-minute consumer in flow.Consumer
// (core/build.go). That wrapper's deferred cancel() runs before its deferred
// close(inner) — defers are LIFO, and flow.go registers close(inner) first —
// so when the pipeline's stream ends cleanly, the inner consumer observes
// ctx.Done() before (or racing) the close of its recv channel. Per the sdk
// contract those are opposite signals: recv closing means "upstream done,
// finish up and flush", ctx.Done means "abandoned, stop now". The inner
// consumer here follows the contract exactly and still gets told it was
// abandoned. The unit-level mechanism test is
// stdlib/flow/flow_test.go:TestConsumerCleanEndIsNotAbandonment.
func Test_RateLimitedConsumer_FinalFlushSurvivesCleanShutdown(t *testing.T) {
	for i := 0; i < 20; i++ {
		var flushed atomic.Int64
		buffering := func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			defer close(done)
			defer close(errs)
			buf := 0
			for {
				select {
				case _, ok := <-recv:
					if !ok {
						flushed.Store(int64(buf)) // clean end: final flush
						return
					}
					buf++
				case <-ctx.Done():
					return // abandoned: buffered work is dropped
				}
			}
		}

		// 6_000_000/min = 10µs period: the limiter is active (so the
		// flow.Consumer wrap is real, exactly as BuildPipeline applies it)
		// without slowing the test.
		if err := panicSafeRun(t, &Pipeline{
			Producers:   staticSource(emitN(3, []byte("x"), nil)),
			Parallel:    1,
			Consumers:   []sdk.Consumer{flow.Consumer(buffering, 6_000_000, 0)},
			Transformer: sdk.Map(func(msg []byte) ([]byte, error) { return msg, nil }),
		}); err != nil {
			t.Fatal(err)
		}
		if n := flushed.Load(); n != 3 {
			t.Fatalf("run %d: clean shutdown dropped the final flush: flushed %d, want 3", i, n)
		}
	}
}
