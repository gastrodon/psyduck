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

	"github.com/gastrodon/psyduck/stdlib/flow"
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
//   - the #10 scenario in Test_ConsumerStopAfter_NoDeadlock hung the old
//     engine indefinitely (confirmed blocked past a 3s bound): the
//     stop-after wrapper stopped reading its recv channel without
//     draining it, so RunPipeline's forward loop blocked forever on a send
//     nobody would ever read again.
//   - the #12 scenario in Test_NilMessage_DoesNotTruncateStream silently
//     dropped every message after the nil — including ones from producers
//     that hadn't even sent it — because old joinProducers used msg == nil
//     on the joined channel as its "all producers closed" sentinel.
//
// #8 (produce-from seed closing without sending) is a parse-layer bug, not
// a core-engine one; its regression test lives in
// parse/hcl/hcl_test.go:TestParseProduceFromClosedSeed.
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
		Producers:   []sdk.Producer{producer},
		Consumers:   []sdk.Consumer{consumer},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
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

// Regression for #10: a consumer whose stop-after cutoff is smaller than
// what the producer sends used to deadlock the pipeline permanently — the
// stop-after wrapper broke out of its receive loop without draining the
// rest, so whatever kept sending into it blocked forever on a send nobody
// would ever read again.
func Test_ConsumerStopAfter_NoDeadlock(t *testing.T) {
	var got atomic.Int64
	err := panicSafeRun(t, &Pipeline{
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
		Producers:   producers,
		Consumers:   []sdk.Consumer{consumer},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
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
			Producers: []sdk.Producer{
				emitN(50, []byte("x"), nil),
				func(_ context.Context, send chan<- []byte, errs chan<- error) {
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

// Capability test, not a regression: sdk v0.5.1 added ctx to Producer and
// Consumer specifically so a plugin abandoned mid-send has a way to exit
// instead of parking forever — the one leak PR #20's rewrite documented as
// unavoidable ("the sdk contract has no context"). A producer that actually
// selects on ctx.Done() alongside its send, cut off mid-stream by
// StopAfter, must leave no goroutine behind at all.
func Test_CtxAwareProducer_LeavesNoGoroutineOnAbandon(t *testing.T) {
	blockForever := func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		for {
			select {
			case send <- []byte("x"):
			case <-ctx.Done():
				return
			}
		}
	}

	baseline := runtime.NumGoroutine()
	var got atomic.Int64
	if err := panicSafeRun(t, &Pipeline{
		Producers:   []sdk.Producer{blockForever},
		Consumers:   []sdk.Consumer{countAll(&got)},
		Transformer: func(msg []byte) ([]byte, error) { return msg, nil },
		StopAfter:   3,
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	t.Fatalf("ctx-aware producer still leaked a goroutine on abandon: %d -> %d\n%s",
		baseline, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
}
