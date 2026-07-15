// Package flow holds the pipeline flow-control combinators: rate limiting, a
// producer-only stop-after cutoff, and the head/tail/sample/throttle/wait
// gates. It is the single home for pacing and pruning a message stream — the
// core engine applies host-owned BlockMeta (per-minute on both verbs,
// stop-after on producers only) through Producer/Consumer, and the stdlib
// flow transformers wrap the same gate helpers, so both paths share one
// implementation.
package flow

import (
	"context"
	"time"

	"github.com/psyduck-etl/sdk"
)

// shutdownGrace bounds how long the Consumer wrapper waits for an inner
// plugin to react to a shutdown signal before giving up. See Consumer's stop()
// for why the wait can't be unconditional (a plugin that never selects on
// ctx.Done() would hang the wrapper forever) or omitted entirely (a plugin
// mid-teardown needs a real chance to finish).
const shutdownGrace = 200 * time.Millisecond

// Limiter returns a wait() that paces calls to at most perMinute and perSecond.
// Non-positive limits are unrestricted; when both are unset wait() never blocks.
func Limiter(perMinute, perSecond int) func() {
	var waits []func()
	if perMinute > 0 {
		t := time.NewTicker(time.Minute / time.Duration(perMinute))
		waits = append(waits, func() { <-t.C })
	}
	if perSecond > 0 {
		t := time.NewTicker(time.Second / time.Duration(perSecond))
		waits = append(waits, func() { <-t.C })
	}
	switch len(waits) {
	case 0:
		return func() {}
	case 1:
		return waits[0]
	default:
		return func() {
			for _, w := range waits {
				w()
			}
		}
	}
}

// Producer wraps p with rate limiting and a stop-after cutoff. With all limits
// unset it returns p unchanged. The wrapper derives an inner ctx that is
// cancelled on any exit path (cutoff, cancellation, inner-close), so p —
// which the sdk contract requires to select on ctx.Done() — exits promptly
// at the cutoff rather than parking on a send into the abandoned inner
// channel until the whole pipeline ends. Live-subscription producers
// (websocket listeners, long-poll loops) then get their teardown for free
// from block-level stop-after.
func Producer(p sdk.Producer, perMinute, perSecond, stopAfter int) sdk.Producer {
	if perMinute <= 0 && perSecond <= 0 && stopAfter <= 0 {
		return p
	}
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		inner := make(chan []byte)
		go p(ctx, inner, errs)

		wait, count := Limiter(perMinute, perSecond), 0
		for {
			select {
			case msg, ok := <-inner:
				if !ok {
					return
				}
				wait()
				select {
				case send <- msg:
				case <-ctx.Done():
					return
				}
				count++
				if stopAfter > 0 && count >= stopAfter {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// Consumer wraps c with rate limiting. With no limit set it returns c
// unchanged. The wrapper derives an inner ctx that is cancelled on any
// exit path so c exits promptly even if it was mid-fetch on an external
// system.
//
// Consumers have no stop-after cutoff: a consumer's own completion is its
// own to decide, and it signals early finish by closing done itself (core's
// sink honors it) — a host-imposed cutoff has nothing to add there.
func Consumer(c sdk.Consumer, perMinute, perSecond int) sdk.Consumer {
	if perMinute <= 0 && perSecond <= 0 {
		return c
	}
	return func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		inner := make(chan []byte)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		innerDone := make(chan struct{})
		go func() {
			defer close(innerDone)
			c(ctx, inner, errs, done)
		}()

		// stop delivers the clean-shutdown signal (close(inner)) and gives the
		// inner consumer a bounded grace window to act on it before falling
		// back to ctx cancellation (abandonment). The two signals can't just
		// fire together: after this wrapper forwards a message into inner,
		// this goroutine keeps running immediately while the woken inner
		// consumer is only marked runnable, not yet scheduled — an
		// unconditional cancel() right after close(inner) can reach the inner
		// consumer's select before it even resumes, and select picks
		// randomly between two simultaneously-ready cases. A consumer
		// actively selecting on inner/ctx.Done() finishes and closes
		// innerDone well within the grace window; only a consumer that isn't
		// listening on inner at all (mid-fetch on external work) ever waits
		// out the window and gets cancelled. The post-cancel wait is bounded
		// too: a consumer that ignores ctx entirely (ill-behaved) must not
		// wedge this wrapper forever — same convention as Producer.
		stop := func() {
			close(inner)
			select {
			case <-innerDone:
				return
			case <-time.After(shutdownGrace):
			}
			cancel()
			select {
			case <-innerDone:
			case <-time.After(shutdownGrace):
			}
		}

		wait := Limiter(perMinute, perSecond)
		for {
			select {
			case msg, ok := <-recv:
				if !ok {
					stop()
					return
				}
				wait()
				select {
				case inner <- msg:
				case <-ctx.Done():
					stop()
					return
				}
			case <-ctx.Done():
				stop()
				return
			}
		}
	}
}

// ── transformer gates (the stdlib flow transformers) ────────────────────────
//
// The stateless gates (Wait, Throttle) lift a per-message func onto the
// contract with sdk.Map. Head/Tail/Sample keep their raw channel loops: each
// carries a counter that must stay invocation-local, which a shared sdk.Map
// closure cannot express.

// Wait sleeps a fixed duration before passing each message through.
func Wait(ms int) sdk.Transformer {
	d := time.Duration(ms) * time.Millisecond
	return sdk.Map(func(msg []byte) ([]byte, error) {
		time.Sleep(d)
		return msg, nil
	})
}

// Throttle rate-limits the stream to perSecond messages, blocking as needed.
func Throttle(perSecond int) sdk.Transformer {
	wait := Limiter(0, perSecond)
	return sdk.Map(func(msg []byte) ([]byte, error) {
		wait()
		return msg, nil
	})
}

// Head passes the first count messages through and drops the rest.
func Head(count int) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		seen := 0
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				if seen >= count {
					continue
				}
				seen++
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
}

// Tail drops the first skip messages and passes the rest through.
func Tail(skip int) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		seen := 0
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				if seen < skip {
					seen++
					continue
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
}

// Sample keeps one message in every rate (rate <= 1 keeps everything).
func Sample(rate int) sdk.Transformer {
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		n := 0
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				keep := rate <= 1 || n%rate == 0
				n++
				if !keep {
					continue
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
}
