package core

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/psyduck-etl/sdk"
)

// ErrNoProducers marks a run whose producer stream yielded nothing to run —
// a produce-from seed that timed out or closed without ever declaring a
// producer, or an otherwise empty stream. It is always fatal, independent of
// exit-on-error: a pipeline that never produced anything did not run.
var ErrNoProducers = errors.New("pipeline ran zero producers")

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
// others. parallel is clamped up to 1 (a zero value from a hand-built
// pipeline would otherwise stall forever).
//
// feedErrs carries bind and stream errors from the feeder (see
// producerSource); they are forwarded into the merged stream so the engine's
// error reporting sees them. Draining feedErrs to its close also joins the
// feeder — and with it the release of any produce-from seed — before the
// stream ends.
//
// If no producer was ever pulled by the time both the workers and the feeder
// are done, the stream ends with ErrNoProducers (wrapping the last feeder
// error, if any, as the cause). The stream also ends when ctx does or when
// the caller breaks out of the loop; in every case all workers and the error
// forwarder are released, not orphaned.
func produce(ctx context.Context, feed <-chan sdk.Producer, feedErrs <-chan error, parallel int) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		if parallel < 1 {
			parallel = 1
		}

		merged := make(chan result)
		var workersWG, errFwdWG sync.WaitGroup
		var got atomic.Int64
		var lastFeedErr error // written by the forwarder, read after its Wait

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
						got.Add(1)
						runProducer(ctx, p, merged)
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		// One forwarder relays feeder errors into the merged stream and
		// records the last one seen. It drains feedErrs until close (not just
		// until ctx ends) so its own return witnesses the feeder's teardown;
		// once ctx is done it keeps draining but stops emitting.
		errFwdWG.Add(1)
		go func() {
			defer errFwdWG.Done()
			for err := range feedErrs {
				if err != nil {
					lastFeedErr = err
					emit(ctx, merged, result{err: err})
				}
			}
		}()

		go func() {
			workersWG.Wait() // every worker saw feed close or ctx end
			errFwdWG.Wait()  // the feeder closed feedErrs; it has released
			if got.Load() == 0 && ctx.Err() == nil {
				err := error(ErrNoProducers)
				if lastFeedErr != nil {
					err = fmt.Errorf("%w: %w", ErrNoProducers, lastFeedErr)
				}
				emit(ctx, merged, result{err: err})
			}
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
// the producer's completion signal, and errors keep flowing until the
// producer function itself returns. Plugins are not required to close their
// errs channel, so the error forwarder instead watches for the producer's
// return — once the function has returned no sender can exist, so a final
// non-blocking drain of errs is race-free and no error sent before returning
// is ever lost. A producer that ignores ctx may leak its own goroutine, but
// both forwarders select on ctx.Done(), so runProducer itself always returns
// on cancellation and the worker moves on.
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
				for {
					select {
					case err, ok := <-errs:
						if !ok {
							return
						}
						if err != nil && !emit(ctx, merged, result{err: err}) {
							return
						}
					default:
						return
					}
				}
			}
		}
	}()

	wg.Wait()
}
