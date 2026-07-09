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

// produce merges producers into a single stream of (message, error) pairs.
//
// Each producer runs in its own goroutine against fresh channels and the
// pipeline's ctx, so an abandoned producer is expected to notice
// cancellation and exit — the sdk contract requires plugins to select on
// ctx.Done() alongside their sends. Two forwarders bridge each producer into
// the merged stream: closing the data channel is the producer's completion
// signal, and errors keep flowing until the producer function itself
// returns. Plugins are not required to close their errs channel, so the
// error forwarder instead watches for the producer's return — once the
// function has returned no sender can exist, so a final non-blocking drain
// of errs is race-free and no error sent before returning is ever lost.
// (Corollary: a producer should return promptly after closing its data
// channel; the stream does not end until it does, or ctx does.) The stream
// ends when every producer has completed, when ctx ends, or when the caller
// breaks out of the loop — in every case all forwarders are released, not
// orphaned.
func produce(ctx context.Context, producers []sdk.Producer) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		merged := make(chan result)
		var dataWG, errsWG sync.WaitGroup

		for _, p := range producers {
			data, errs := make(chan []byte), make(chan error)
			returned := make(chan struct{}) // closed when the producer function returns
			go func() {
				defer close(returned)
				p(ctx, data, errs)
			}()

			dataWG.Add(1)
			go func() {
				defer dataWG.Done()
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

			// Errors keep flowing until the producer function returns.
			// After `returned` fires no sender exists, so the final
			// non-blocking drain cannot race a send — every error emitted
			// before the producer returned is delivered.
			errsWG.Add(1)
			go func() {
				defer errsWG.Done()
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
		}

		go func() {
			dataWG.Wait() // every producer closed its data channel (or ctx ended)
			errsWG.Wait() // every producer returned (or ctx ended); no sender left
			close(merged)
		}()

		for r := range merged {
			if !yield(r.msg, r.err) {
				return // defer cancel unwinds the forwarders
			}
		}
	}
}
