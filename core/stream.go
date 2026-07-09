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
// signal, while errors flow for as long as any data path is live. Plugins
// are not required to close their errs channel, so error forwarders don't
// wait on one — once all data channels close they sweep pending errors and
// exit. The stream ends when every producer has completed, when ctx ends,
// or when the caller breaks out of the loop — in every case all forwarders
// are released, not orphaned.
func produce(ctx context.Context, producers []sdk.Producer) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		merged := make(chan result)
		dataDone := make(chan struct{}) // closed once every data channel has closed
		var dataWG, errsWG sync.WaitGroup

		for _, p := range producers {
			data, errs := make(chan []byte), make(chan error)
			go p(ctx, data, errs)

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

			// Errors received before the producer completes are always
			// delivered. Once dataDone fires the forwarder sweeps whatever
			// is already pending and exits — an error a plugin emits after
			// closing its data channel races that sweep, best-effort by
			// necessity since plugins need not close errs at all.
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
					case <-dataDone:
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
			dataWG.Wait()   // every producer closed its data channel (or ctx ended)
			close(dataDone) // error forwarders sweep and exit
			errsWG.Wait()   // no sender left before merged closes
			close(merged)
		}()

		for r := range merged {
			if !yield(r.msg, r.err) {
				return // defer cancel unwinds the forwarders
			}
		}
	}
}
