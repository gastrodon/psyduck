package core

import (
	"context"
	"iter"
	"sync"

	"github.com/psyduck-etl/sdk"
)

// result carries one producer emission — a message or an error — through
// the fan-in channel behind produce. It is transport only; the engine
// consumes the pairs through iter.Seq2.
type result struct {
	msg []byte
	err error
}

// emit sends r into out unless ctx ends first. It reports whether the send
// landed.
func emit(ctx context.Context, out chan<- result, r result) bool {
	select {
	case out <- r:
		return true
	case <-ctx.Done():
		return false
	}
}

// produce runs a stream of producers through a fixed worker pool and merges
// their output into a single stream of (message, error) pairs.
//
// parallel workers pull producers off feed; each runs one producer to full
// exhaustion (see runProducer) and then pulls the next — replace-on-
// exhaustion, so a finished producer's slot refills immediately from the
// next arrival. There are no waves and no ordering guarantee across
// producers: a long-lived producer occupies one slot without blocking the
// others. parallel must be >= 1 — the parser guarantees a positive value, so
// callers pass it straight through. A zero would start no workers and stall.
//
// feedErrs carries bind and stream errors from the feeder (see
// producerSource) — including a produce-from seed that timed out or closed
// without ever declaring a producer. They are forwarded into the merged stream
// as ordinary errors, so the engine's error reporting (and exit-on-error)
// treats them like any other producer error. Draining feedErrs to its close
// also joins the feeder — and with it the release of any produce-from seed —
// before the stream ends.
//
// The stream ends when feed and feedErrs both close (the feeder is done), when
// ctx does, or when the caller breaks out of the loop; in every case all
// workers and the error forwarder are released, not orphaned. A stream that
// yielded no producers simply ends without emitting anything.
func produce(ctx context.Context, feed <-chan sdk.Producer, feedErrs <-chan error, parallel int) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		merged := make(chan result)
		var workersWG, errFwdWG sync.WaitGroup

		for range parallel {
			workersWG.Add(1)
			go func() {
				defer workersWG.Done()
				for {
					select {
					case p, ok := <-feed:
						if !ok {
							return // stream exhausted
						}
						runProducer(ctx, p, merged)
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		// One forwarder relays feeder errors into the merged stream. It drains
		// feedErrs until close (not just until ctx ends) so its own return
		// witnesses the feeder's teardown; once ctx is done it keeps draining
		// but stops emitting.
		errFwdWG.Add(1)
		go func() {
			defer errFwdWG.Done()
			for err := range feedErrs {
				if err != nil {
					emit(ctx, merged, result{err: err})
				}
			}
		}()

		go func() {
			workersWG.Wait() // every worker saw feed close or ctx end
			errFwdWG.Wait()  // the feeder closed feedErrs; it has released
			close(merged)
		}()

		for r := range merged {
			if !yield(r.msg, r.err) {
				return // defer cancel unwinds the workers and forwarder
			}
		}
	}
}

// runProducer runs a single producer to completion, forwarding its messages
// and errors into merged, and returns only once the producer is fully done.
//
// Two forwarders bridge the producer into merged: closing the data channel is
// the producer's completion signal, and errors flow until the producer
// function returns. Plugins are not required to close their errs channel, so
// the error forwarder also watches for the producer's return. errs is
// unbuffered, so any error the producer sends is received before the producer
// function can return — once returned is closed no un-received error can
// remain, so the forwarder simply stops. A producer that ignores ctx may leak
// its own goroutine, but both forwarders select on ctx.Done(), so runProducer
// itself always returns on cancellation and the worker moves on.
func runProducer(ctx context.Context, p sdk.Producer, merged chan<- result) {
	data, errs := make(chan []byte), make(chan error)
	returned := make(chan struct{}) // closed when the producer function returns
	go func() {
		defer close(returned)
		p(ctx, data, errs)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case msg, ok := <-data:
				if !ok {
					return
				}
				if !emit(ctx, merged, result{msg: msg}) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case err, ok := <-errs:
				if !ok {
					return
				}
				if err != nil && !emit(ctx, merged, result{err: err}) {
					return
				}
			case <-ctx.Done():
				return
			case <-returned:
				return
			}
		}
	}()

	wg.Wait()
}
