// Package flow holds the pipeline flow-control combinators: rate limiting,
// stop-after cutoffs, and the head/tail/sample/throttle/wait gates. It is the
// single home for pacing and pruning a message stream — the core engine applies
// host-owned BlockMeta (per-minute, stop-after) through Producer/Consumer, and
// the stdlib flow transformers wrap the same gate helpers, so both paths share
// one implementation.
package flow

import (
	"context"
	"sync"
	"time"

	"github.com/psyduck-etl/sdk"
)

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
// unset it returns p unchanged. p receives the same ctx as the wrapper — the
// sdk contract requires it to select on ctx.Done() on its own — and the
// wrapper's own relay to send also honors ctx, so an abandoned wrapped
// producer never blocks past cancellation.
func Producer(p sdk.Producer, perMinute, perSecond, stopAfter int) sdk.Producer {
	if perMinute <= 0 && perSecond <= 0 && stopAfter <= 0 {
		return p
	}
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
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

// Consumer wraps c with rate limiting and a stop-after cutoff. With all limits
// unset it returns c unchanged. c receives the same ctx as the wrapper, and
// the wrapper's own relay from recv also honors ctx.
//
// At the cutoff (or on cancellation) the wrapper stops receiving and closes
// the inner stream; c flushes and closes done. The wrapper deliberately does
// not drain recv — done is the host's signal to stop sending (core's sink
// honors it), and silently discarding messages would hide the cutoff from
// the host and keep upstream producing into the void.
func Consumer(c sdk.Consumer, perMinute, perSecond, stopAfter int) sdk.Consumer {
	if perMinute <= 0 && perSecond <= 0 && stopAfter <= 0 {
		return c
	}
	return func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		inner := make(chan []byte)
		go c(ctx, inner, errs, done)
		defer close(inner)

		wait, count := Limiter(perMinute, perSecond), 0
		for {
			select {
			case msg, ok := <-recv:
				if !ok {
					return
				}
				wait()
				select {
				case inner <- msg:
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

// ── transformer gates (shared by the stdlib flow transformers) ──────────────

// Wait sleeps a fixed duration before passing each message through.
func Wait(ms int) sdk.Transformer {
	d := time.Duration(ms) * time.Millisecond
	return func(in []byte) ([]byte, error) {
		time.Sleep(d)
		return in, nil
	}
}

// Throttle rate-limits the stream to perSecond messages, blocking as needed.
func Throttle(perSecond int) sdk.Transformer {
	wait := Limiter(0, perSecond)
	return func(in []byte) ([]byte, error) {
		wait()
		return in, nil
	}
}

// Head passes the first count messages through and drops the rest.
func Head(count int) sdk.Transformer {
	var mu sync.Mutex
	seen := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen >= count {
			return nil, nil
		}
		seen++
		return in, nil
	}
}

// Tail drops the first skip messages and passes the rest through.
func Tail(skip int) sdk.Transformer {
	var mu sync.Mutex
	seen := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen < skip {
			seen++
			return nil, nil
		}
		return in, nil
	}
}

// Sample keeps one message in every rate (rate <= 1 keeps everything).
func Sample(rate int) sdk.Transformer {
	if rate <= 1 {
		return func(in []byte) ([]byte, error) { return in, nil }
	}
	var mu sync.Mutex
	n := 0
	return func(in []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		keep := n%rate == 0
		n++
		if keep {
			return in, nil
		}
		return nil, nil
	}
}
