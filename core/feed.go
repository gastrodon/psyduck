package core

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/stdlib/flow"
)

// ProducerSource starts a producer feeder and hands back the channels it
// drives. Calling it launches one goroutine that drains the underlying
// resource stream, binds each parsed resource against its plugin, and
// delivers the resulting producers on the first channel; bind and stream
// errors arrive on the second.
//
// The producer channel closes once the stream is exhausted (or a terminal
// stream error or ctx ends it). The error channel closes only after the
// feeder has released the underlying stream — so a produce-from seed is
// always stopped and joined before the error channel closes, which is what
// lets the pool observe teardown deterministically.
type ProducerSource func(ctx context.Context) (<-chan sdk.Producer, <-chan error)

// producerSource turns a parsed resource stream into a ProducerSource that
// binds lazily, at run time. Both literal (produce = [...]) and dynamic
// (produce-from) pipelines flow through here unchanged — a literal stream
// drains instantly, a seeded stream keeps yielding as its seed sends.
//
// The feeder is the sole caller of bindings: pulls and the max < 1 release
// are serialized on one goroutine, since a ResourceFunc (a produce-from
// seed) is not reentrant. Anchoring the first pull to ctx makes the seed a
// child of the run ctx, so cancellation propagates down to it for free.
//
// Both channels are unbuffered: a saturated worker pool blocks the feeder,
// which blocks the pull, which is backpressure onto the seed — no unbounded
// queue of pending producers builds up.
func producerSource(bindings parse.ResourceFunc, plugins map[string]sdk.Plugin) ProducerSource {
	return func(ctx context.Context) (<-chan sdk.Producer, <-chan error) {
		feed := make(chan sdk.Producer)
		errs := make(chan error)

		go func() {
			// Defers run LIFO: release the stream first (stopping and
			// joining a produce-from seed), then close feed, then close
			// errs. Releasing before feed closes means a test watching for
			// release sees it strictly before the pool reports exhaustion.
			defer close(errs)
			defer close(feed)
			defer bindings(context.Background(), 0)

			send := func(p sdk.Producer) bool {
				select {
				case feed <- p:
					return true
				case <-ctx.Done():
					return false
				}
			}
			fail := func(err error) bool {
				select {
				case errs <- err:
					return true
				case <-ctx.Done():
					return false
				}
			}

			for {
				chunk, err := bindings(ctx, bindChunk)
				if err != nil {
					fail(err) // a stream error is terminal: the stream is dead
					return
				}
				if len(chunk) == 0 {
					return // exhausted
				}
				for _, b := range chunk {
					p, err := bindProducer(b, plugins)
					if err != nil {
						// a bind error is non-terminal: report it, skip this
						// resource, and keep feeding whatever else arrives
						if !fail(err) {
							return
						}
						continue
					}
					if !send(p) {
						return
					}
				}
			}
		}()

		return feed, errs
	}
}

// bindProducer resolves b against its owning plugin and wraps the instance
// with its host-owned flow gates (per-minute, stop-after).
func bindProducer(b parse.Resource, plugins map[string]sdk.Plugin) (sdk.Producer, error) {
	plugin, ok := plugins[b.PluginID]
	if !ok {
		return nil, fmt.Errorf("%s: %s: no plugin %q loaded", b.Block.Origin(), b.Ref, b.PluginID)
	}
	instance, err := plugin.Bind(b.Kind, b.Resource.Name, b.Block)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", b.Block.Origin(), b.Ref, err)
	}
	return flow.Producer(instance.Produce, b.Meta.PerMinute, 0, b.Meta.StopAfter), nil
}
